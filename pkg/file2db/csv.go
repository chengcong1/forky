package file2db

import (
	"bufio"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

// CSVSource 流式读取 CSV/TSV 文本文件，自动识别常见编码与 BOM。
type CSVSource struct {
	file *os.File
	r    *csv.Reader

	cfg      readConfig // 读取配置：表头行号、跳过行、无表头模式等
	path     string     // 文件路径，默认表名由它推导，报错时也用它定位
	encoding string     // 实际使用的编码名
}

// NewCSV 打开 CSV 文件并创建流式数据源，用完请 Close。
//
//	WithDelimiter('\t')、WithEncoding(file2db.EncodingGBK)、WithHeaderRow(2)、WithNoHeader()
func NewCSV(path string, opts ...CSVOption) (*CSVSource, error) {
	cfg := defaultReadConfig()
	for _, o := range opts {
		o(&cfg)
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("file2db: 打开 CSV 失败 %s: %w", path, err)
	}

	br := bufio.NewReaderSize(f, 64*1024)
	// log.Println("file2db: 识别编码为1", cfg.Encoding)
	reader, encName, err := decodeReader(br, cfg.Encoding)
	// log.Println("file2db: 识别编码为2", encName)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("file2db: 识别 %s 编码失败: %w", path, err)
	}

	r := csv.NewReader(reader)
	r.Comma = cfg.Delimiter
	r.Comment = cfg.Comment
	r.LazyQuotes = cfg.LazyQuotes
	r.TrimLeadingSpace = cfg.TrimLeadingSpace
	r.ReuseRecord = cfg.ReuseRecord
	r.FieldsPerRecord = cfg.FieldsPerRecord

	return &CSVSource{file: f, r: r, cfg: cfg, path: path, encoding: encName}, nil
}

// Import 把 CSV/TSV 流式导入 SQLite。ctx 控制超时与取消。
//
// 与 Excel 的区别：
//   - 只有一张表，表名默认取文件名（去目录与扩展名），可用 WithTable / WithDefaultTable 覆盖；
//   - 表头定位同样按 WithHeaderRow / WithSkipRows / WithNoHeader 处理，
//     列名默认清洗，WithRawHeader() 可原样保留；
//   - 行尾缺列会自动补空串，不会因参数个数不匹配而报错。
func (s *CSVSource) Import(ctx context.Context, db Database, opts ...ImportOption) (*Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if db == nil {
		return nil, errors.New("file2db: db 不能为 nil")
	}
	if s.file == nil {
		return nil, errors.New("file2db: file 不能为 nil")
	}

	cfg := defaultOptions()
	for _, o := range opts {
		o(&cfg)
	}

	res, err := s.importCSV(ctx, db, cfg)
	if err != nil {
		return nil, fmt.Errorf("file2db: 导入 CSV %s 失败: %w", s.path, err)
	}
	if res == nil {
		return nil, errors.New("file2db: 文件中没有可导入的数据")
	}
	return res, nil
}

// importCSV 导入单个 CSV 文件：表头定位、建表、批量事务插入。
// 返回 nil, nil 表示文件为空（读不到任何非空行），调用方应报错。
func (s *CSVSource) importCSV(ctx context.Context, db Database, cfg ImpOptions) (*Result, error) {
	rawHeader, pending, err := readHeaderRows(s.nextRaw, s.cfg)
	if err != nil {
		return nil, err
	}
	if rawHeader == nil {
		return nil, nil // 空文件
	}

	table := cfg.Table
	if table == "" {
		table = cfg.defaultTable
	}
	if table == "" {
		// CSV 没有 Sheet 名，默认用文件名做表名
		table = TableNameFromPath(s.path)
	}
	table = SanitizeTableName(table)

	// 默认把列名清洗成小写下划线；WithRawHeader() 时原样保留表头文字
	headers := CleanHeaders(rawHeader, s.cfg.KeepRawHeader)
	return writeRows(ctx, db, pickDialect(cfg.Dialect, db), fmt.Sprintf("CSV %q", s.path), table, headers, cfg, s.nextRaw, pending)
}

// nextRaw 读取下一条 CSV 记录，读完返回 io.EOF。
// encoding/csv 会自动跳过完全空白的行。
func (s *CSVSource) nextRaw() ([]string, error) {
	rec, err := s.r.Read()
	if err != nil {
		return nil, err // 含 io.EOF
	}
	return rec, nil
}

// NewTSV 等价于 NewCSV 并指定制表符分隔。
func NewTSV(path string, opts ...CSVOption) (*CSVSource, error) {
	return NewCSV(path, append([]CSVOption{WithDelimiter('\t')}, opts...)...)
}

// Encoding 返回实际使用的编码名。
func (s *CSVSource) Encoding() string { return s.encoding }

// Close 实现 io.Closer。
func (s *CSVSource) Close() error {
	if s.file != nil {
		err := s.file.Close()
		s.file = nil
		if err != nil {
			return fmt.Errorf("file2db: 关闭 CSV: %w", err)
		}
	}
	return nil
}

// ---------- 编码识别 ----------

// decodeReader 按需把字节流包装成解码后的 UTF-8 读取器，并返回识别到的编码名。
// 会顺带消费掉文件开头的 BOM。
func decodeReader(br *bufio.Reader, want string) (io.Reader, string, error) {
	head, err := br.Peek(4096)
	if err != nil && err != io.EOF && err != bufio.ErrBufferFull {
		return nil, "", err
	}
	if len(head) == 0 {
		return br, want, nil
	}

	if want == "" || want == EncodingAuto {
		want = sniffEncoding(head)
	}

	switch strings.ToLower(want) {
	case "_utf-8", "utf8", "utf-8":
		want = EncodingUTF8
	case "gb2312", "gb-2312":
		want = EncodingGBK
	case "utf-16", "utf16", "utf16le":
		want = EncodingUTF16LE
	case "utf16be":
		want = EncodingUTF16BE
	}

	var dec encoding.Encoding
	switch strings.ToLower(want) {
	case EncodingUTF8, "ascii", "":
		// UTF-8 / ASCII 无需转码
		dec = nil
	case EncodingUTF16LE:
		dec = unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM)
	case EncodingUTF16BE:
		dec = unicode.UTF16(unicode.BigEndian, unicode.IgnoreBOM)
	case EncodingGBK, EncodingGB18030:
		// GB18030 是 GBK 的超集，用它可以兼容两种文件的解码
		dec = simplifiedchinese.GB18030
		want = EncodingGB18030
	default:
		return nil, "", fmt.Errorf("不支持的编码 %q（可选：auto/utf-8/gbk/gb18030/utf-16le/utf-16be）", want)
	}

	stripBOM(br, head, want)

	if dec == nil {
		return br, EncodingUTF8, nil
	}
	return transform.NewReader(br, dec.NewDecoder()), want, nil
}

// stripBOM 根据编码名丢弃开头的 BOM 字节。
func stripBOM(br *bufio.Reader, head []byte, want string) {
	var bom []byte
	switch strings.ToLower(want) {
	case EncodingUTF8:
		bom = []byte{0xEF, 0xBB, 0xBF}
	case EncodingUTF16LE:
		bom = []byte{0xFF, 0xFE}
	case EncodingUTF16BE:
		bom = []byte{0xFE, 0xFF}
	default:
		return
	}
	if bytes.HasPrefix(head, bom) {
		_, _ = br.Discard(len(bom))
	}
}

// sniffEncoding 通过 BOM 与 UTF-8 合法性推断编码。
func sniffEncoding(head []byte) string {
	switch {
	case bytes.HasPrefix(head, []byte{0xEF, 0xBB, 0xBF}):
		return EncodingUTF8
	case bytes.HasPrefix(head, []byte{0xFF, 0xFE}):
		return EncodingUTF16LE
	case bytes.HasPrefix(head, []byte{0xFE, 0xFF}):
		return EncodingUTF16BE
	}
	if looksUTF8(head) {
		return EncodingUTF8
	}
	// 国内网管导出的 CSV 大多是 GBK/GB18030
	return EncodingGB18030
}

// looksUTF8 判断样本是否为 UTF-8；Peek 可能截断到半个多字节字符，
// 因此允许末尾最多 3 个字节不完整。
func looksUTF8(b []byte) bool {
	if utf8.Valid(b) {
		return true
	}
	for i := 1; i <= 3 && i < len(b); i++ {
		if utf8.Valid(b[:len(b)-i]) {
			return true
		}
	}
	return false
}

// HexPreview 调试用：返回文件头若干字节的十六进制串，便于排查编码问题。
func HexPreview(path string, n int) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	buf := make([]byte, n)
	read, _ := io.ReadFull(f, buf)
	return hex.EncodeToString(buf[:read]), nil
}
