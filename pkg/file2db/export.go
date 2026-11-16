package file2db

// 导出：把库里的表流式写成 CSV / XLSX。
//
// 设计要点：
//  1. 全程流式：CSV 走 bufio + encoding/csv，Excel 走 excelize.StreamWriter
//     （逐行落盘，不像 SetSheetRow 那样把整张表留在内存），内存占用与表大小无关；
//  2. 缓冲复用：扫描指针、转换后的字符串、Excel 的 []any 都只分配一次，
//     类型转换走 strconv 快路径，不产生按行的切片分配；
//  3. 只 SELECT 需要的列：不导出主键时按列清单查询，主键列根本不会被读出来；
//  4. 写完显式检查 Flush / 编码器刷尾，避免「最后几行没落盘却返回 nil」；
//  5. 产物要在内存里而不是文件时用 ExportSlice（见 slice.go），它复用同一套查询与
//     逐行转换，只把写入端换成 [][]string。
import (
	"bufio"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

// ExportFormat 导出格式。
type ExportFormat string

const (
	FormatCSV  ExportFormat = "csv"
	FormatXLSX ExportFormat = "xlsx"
)

// Ext 返回该格式的文件扩展名（含点）。
func (f ExportFormat) Ext() string {
	if f == FormatXLSX {
		return ".xlsx"
	}
	return ".csv"
}

// ExportOptions 导出配置。
type ExportOptions struct {
	Format        ExportFormat // 导出格式；留空时 ExportTable 按输出后缀推断，ExportTables 用 CSV
	Tables        []string     // 只导出指定表，留空表示库中全部用户表
	Index         bool         // 是否导出主键列（导入时自动加的 id 列），默认 true
	SingleFile    string       // 非空时把多张表写进同一个 xlsx（每表一个 Sheet）
	Dialect       Dialect      // 源库方言，留空时从数据源推断
	Delimiter     rune         // CSV：字段分隔符，默认 ','
	Encoding      string       // CSV：输出编码，默认 utf-8，可 gbk/gb18030/utf-16le/utf-16be
	BOM           bool         // CSV：是否写 BOM（UTF-8 带 BOM 后 Excel 才能正确识别中文）
	Logger        *log.Logger  // 进度日志，nil 表示不输出
	ProgressEvery int64        // 每多少行输出一次进度，0 关闭
}

func defaultExportOptions() ExportOptions {
	return ExportOptions{
		Index:     true,
		Delimiter: ',',
		Encoding:  EncodingUTF8,
	}
}

// ExportOption 用于配置导出行为。
type ExportOption func(*ExportOptions)

// WithExportFormat 指定导出格式：FormatCSV（默认）或 FormatXLSX。
func WithExportFormat(f ExportFormat) ExportOption {
	return func(o *ExportOptions) { o.Format = f }
}

// WithExportIndex 指定是否导出主键列（导入时自动加的自增 id），默认导出。
//
// 注意：导入时会自己建一列 id INTEGER PRIMARY KEY，所以「带 id 导出再导入」
// 会让该列被改名为 id_2（与主键重名）。若导出的文件还要回灌，建议传 false。
// 传 false 时列清单里不会有 id，查询也不会去读它。
func WithExportIndex(export bool) ExportOption {
	return func(o *ExportOptions) { o.Index = export }
}

// WithExportTables 只导出指定表；留空导出全部用户表。
func WithExportTables(names ...string) ExportOption {
	return func(o *ExportOptions) { o.Tables = names }
}

// WithExportSingleFile 把多张表导出到同一个 xlsx 文件，每张表一个 Sheet（Sheet 名取表名）。
//
// 传相对路径时按 ExportTables 的 outputDir 解析；只支持 xlsx，
// 传 .csv 这类不能装多表的格式会直接报错（只有 ExportTables 支持单文件）。
//
//	results, err := file2db.ExportTables(ctx, db, "out",
//		file2db.WithExportSingleFile("data.xlsx"),
//		file2db.WithExportIndex(false))
func WithExportSingleFile(fileName string) ExportOption {
	return func(o *ExportOptions) { o.SingleFile = fileName }
}

// WithExportDialect 显式指定源库方言（用 Open 返回的 *DB 时不需要）。
func WithExportDialect(d Dialect) ExportOption {
	return func(o *ExportOptions) { o.Dialect = d }
}

// WithExportDelimiter 指定 CSV 字段分隔符（默认 ','，需要 TSV 时传 '\t'）。
func WithExportDelimiter(r rune) ExportOption {
	return func(o *ExportOptions) {
		if r != 0 {
			o.Delimiter = r
		}
	}
}

// WithExportEncoding 指定 CSV 输出编码：utf-8（默认）、gbk、gb18030、utf-16le、utf-16be。
// 国内用 Excel 打开建议 gbk + 不加 BOM，或 utf-8 + WithExportBOM()。
func WithExportEncoding(name string) ExportOption {
	return func(o *ExportOptions) {
		if name != "" {
			o.Encoding = name
		}
	}
}

// WithExportBOM 在文件开头写入 BOM。
func WithExportBOM() ExportOption {
	return func(o *ExportOptions) { o.BOM = true }
}

// WithExportLogger 输出导出进度日志。
func WithExportLogger(l *log.Logger) ExportOption {
	return func(o *ExportOptions) { o.Logger = l }
}

// WithExportProgress 每 every 行输出一次进度，0 关闭。
func WithExportProgress(every int64) ExportOption {
	return func(o *ExportOptions) {
		if every > 0 {
			o.ProgressEvery = every
		}
	}
}

// ExportResult 描述单张表的导出结果。
type ExportResult struct {
	Table   string        // 表名
	Path    string        // 输出文件路径（单文件模式时多张表同一个路径）
	Sheet   string        // Excel：实际写入的 Sheet 名（CSV 为空）
	Columns []string      // 实际写出的列名（不含被排除的主键）
	Rows    int64         // 写出的数据行数（不含表头）
	Elapsed time.Duration // 耗时
}

// ExportTable 把单张表导出成一个文件（一个表一个文件）。
//
// ctx 控制超时与取消：取消后立即停止查询，并按「不留半截产物」的约定删掉输出文件。
//
// 格式默认按 outputPath 的后缀推断：.xlsx/.xlsm 走 Excel，其余走 CSV；
// 也可以用 WithExportFormat 强制指定。
//
//	res, err := file2db.ExportTable(ctx, db, "data", "out/data.csv",
//		file2db.WithExportIndex(false), file2db.WithExportEncoding(file2db.EncodingGBK))
func ExportTable(ctx context.Context, db Database, table, outputPath string, opts ...ExportOption) (*ExportResult, error) {
	cfg := defaultExportOptions()
	for _, o := range opts {
		o(&cfg)
	}
	if db == nil {
		return nil, errors.New("file2db: db 不能为 nil")
	}
	if table == "" {
		return nil, errors.New("file2db: table 不能为空")
	}
	if outputPath == "" {
		return nil, errors.New("file2db: outputPath 不能为空")
	}
	cfg.Format = resolveExportFormat(cfg.Format, outputPath)
	return exportTable(ctx, db, table, outputPath, cfg)
}

// ExportTables 把库里的用户表导出到 outputDir。
//
// 默认一个表一个文件，文件名取表名（非法字符会替换为下划线），后缀由格式决定；
// 加了 WithExportSingleFile 则全部写进同一个 xlsx，每张表一个 Sheet。
//
// 表清单来自 WithExportTables，留空则导出库中全部用户表。
//
//	results, err := file2db.ExportTables(ctx, db, "out",
//		file2db.WithExportFormat(file2db.FormatXLSX),
//		file2db.WithExportIndex(false))
func ExportTables(ctx context.Context, db Database, outputDir string, opts ...ExportOption) ([]*ExportResult, error) {
	cfg := defaultExportOptions()
	for _, o := range opts {
		o(&cfg)
	}
	if db == nil {
		return nil, errors.New("file2db: db 不能为 nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if outputDir == "" {
		return nil, errors.New("file2db: outputDir 不能为空")
	}
	if cfg.SingleFile == "" {
		cfg.Format = resolveExportFormat(cfg.Format, "") // 目录模式下没有后缀可推断，默认 CSV
	} else {
		// 单文件模式只可能是 xlsx：后缀说了算，没写后缀就补 .xlsx
		if filepath.Ext(cfg.SingleFile) == "" {
			cfg.SingleFile += ".xlsx"
		}
		switch strings.ToLower(filepath.Ext(cfg.SingleFile)) {
		case ".xlsx", ".xlsm":
			cfg.Format = FormatXLSX
		default:
			return nil, fmt.Errorf("file2db: 单文件模式只支持 xlsx（一张表一个 Sheet），%s 装不下多张表",
				cfg.SingleFile)
		}
	}

	tables := cfg.Tables
	if len(tables) == 0 {
		var err error
		if tables, err = Tables(ctx, db); err != nil {
			return nil, fmt.Errorf("file2db: 读取表清单失败: %w", err)
		}
	}
	if len(tables) == 0 {
		return nil, errors.New("file2db: 数据库中没有可导出的表")
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return nil, fmt.Errorf("file2db: 创建输出目录 %s 失败: %w", outputDir, err)
	}

	if cfg.SingleFile != "" {
		return exportWorkbook(ctx, db, tables, outputDir, cfg)
	}

	results := make([]*ExportResult, 0, len(tables))
	for _, table := range tables {
		path := filepath.Join(outputDir, safeFileName(table)+cfg.Format.Ext())
		res, err := exportTable(ctx, db, table, path, cfg)
		if err != nil {
			return results, fmt.Errorf("file2db: 导出表 %q 失败: %w", table, err)
		}
		if cfg.Logger != nil {
			cfg.Logger.Printf("file2db: 导出 %s -> %s（%d 行，耗时 %s）", table, path, res.Rows, res.Elapsed)
		}
		results = append(results, res)
	}
	return results, nil
}

// exportWorkbook 把多张表写进同一个 xlsx，每张表一个 Sheet，最后一次保存。
func exportWorkbook(ctx context.Context, db Database, tables []string, outputDir string, cfg ExportOptions) ([]*ExportResult, error) {
	path := cfg.SingleFile
	if !filepath.IsAbs(path) {
		path = filepath.Join(outputDir, path)
	}
	d := pickDialect(cfg.Dialect, db)

	wb := newXLSXWorkbook(path)
	ok := false
	// 中途失败不留半截文件（xlsx 在 Close 时才 SaveAs，这里主要是释放临时资源）
	defer func() {
		if !ok {
			_ = wb.file.Close()
			_ = os.Remove(path)
		}
	}()

	results := make([]*ExportResult, 0, len(tables))
	for _, table := range tables {
		start := time.Now()
		cols, err := exportColumnsOf(ctx, db, d, table, cfg)
		if err != nil {
			return results, fmt.Errorf("file2db: 导出表 %q 失败: %w", table, err)
		}
		if err := wb.BeginSheet(table); err != nil {
			return results, fmt.Errorf("file2db: 导出表 %q 失败: %w", table, err)
		}
		written, err := writeTableRows(ctx, wb, db, d, table, cols, cfg)
		if err != nil {
			return results, fmt.Errorf("file2db: 导出表 %q 失败: %w", table, err)
		}
		res := &ExportResult{Table: table, Path: path, Sheet: wb.sheet, Columns: cols, Rows: written, Elapsed: time.Since(start)}
		results = append(results, res)
		if cfg.Logger != nil {
			cfg.Logger.Printf("file2db: 导出 %s -> %s[%s]（%d 行，耗时 %s）", table, path, res.Sheet, res.Rows, res.Elapsed)
		}
	}

	if err := wb.Close(); err != nil {
		return results, err
	}
	ok = true
	return results, nil
}

// exportTable 流式导出单表到一个文件：查列 → 建 writer → 写表头 → 逐行写 → 刷盘关闭。
func exportTable(ctx context.Context, db Database, table, path string, cfg ExportOptions) (*ExportResult, error) {
	start := time.Now()
	d := pickDialect(cfg.Dialect, db)

	cols, err := exportColumnsOf(ctx, db, d, table, cfg)
	if err != nil {
		return nil, err
	}

	sink, err := newRowSink(cfg.Format, path, table, cfg)
	if err != nil {
		return nil, err
	}
	// 中途失败时不留半截文件
	ok := false
	defer func() {
		if !ok {
			_ = sink.Close()
			_ = os.Remove(path)
		}
	}()

	written, err := writeTableRows(ctx, sink, db, d, table, cols, cfg)
	if err != nil {
		return nil, err
	}
	if err := sink.Close(); err != nil {
		return nil, fmt.Errorf("file2db: 关闭输出 %s 失败: %w", path, err)
	}
	ok = true

	res := &ExportResult{Table: table, Path: path, Columns: cols, Rows: written, Elapsed: time.Since(start)}
	if cfg.Format == FormatXLSX {
		res.Sheet = sheetName(table)
	}
	return res, nil
}

// exportColumnsOf 校验表存在，并返回实际要导出的列名（WithExportIndex(false) 时不含主键）。
func exportColumnsOf(ctx context.Context, db Database, d Dialect, table string, cfg ExportOptions) ([]string, error) {
	exists, err := TableExists(ctx, db, table)
	if err != nil {
		return nil, fmt.Errorf("file2db: 检查表 %q 失败: %w", table, err)
	}
	if !exists {
		return nil, fmt.Errorf("file2db: 表 %q 不存在", table)
	}

	cols, err := tableInfo(ctx, db, d, table)
	if err != nil {
		return nil, err
	}
	names := exportColumns(cols, cfg.Index)
	if len(names) == 0 {
		return nil, fmt.Errorf("file2db: 表 %q 没有可导出的列", table)
	}
	return names, nil
}

// writeTableRows 写出一张表的表头与全部数据行，返回写出的行数。
func writeTableRows(ctx context.Context, w tableWriter, db Database, d Dialect, table string, cols []string, cfg ExportOptions) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if err := w.WriteRow(cols); err != nil {
		return 0, fmt.Errorf("file2db: 写入表头失败: %w", err)
	}

	// 显式列清单：WithExportIndex(false) 时主键列不会被读出来，省 IO 与转换
	query := "SELECT " + quoteColumnList(d, cols) + " FROM " + d.QuoteIdent(table)
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return 0, fmt.Errorf("file2db: 查询表 %q 失败: %w", table, err)
	}
	defer rows.Close()

	// 复用同一组缓冲：每行只做一次扫描 + 一次类型转换，不产生按行切片分配
	var (
		raw  = make([]any, len(cols))
		ptrs = make([]any, len(cols))
		strs = make([]string, len(cols))
	)
	for i := range raw {
		ptrs[i] = &raw[i]
	}

	var written int64
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return written, fmt.Errorf("file2db: 读取表 %q 第 %d 行失败: %w", table, written+1, err)
		}
		for i, v := range raw {
			strs[i] = anyToString(v)
		}
		if err := w.WriteRow(strs); err != nil {
			return written, fmt.Errorf("file2db: 写入表 %q 第 %d 行失败: %w", table, written+1, err)
		}
		written++
		if cfg.ProgressEvery > 0 && cfg.Logger != nil && written%cfg.ProgressEvery == 0 {
			cfg.Logger.Printf("file2db: %s 已导出 %d 行", table, written)
		}
	}
	if err := rows.Err(); err != nil {
		return written, fmt.Errorf("file2db: 遍历表 %q 失败: %w", table, err)
	}
	return written, nil
}

// anyToString 把驱动返回的值转成要写出的字符串。
// 覆盖 SQLite 会返回的全部类型，走 strconv 快路径而不是 fmt 反射（逐单元格的热点）。
func anyToString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case []byte:
		return string(x) // BLOB/旧驱动可能复用底层数组，必须拷贝一份
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	case time.Time:
		return x.Format(time.RFC3339)
	default:
		return fmt.Sprint(v)
	}
}

// ---------- 写入端 ----------

// tableWriter 是导出主流程看到的写入端：只逐行写，文件 / 工作簿细节由实现自己管。
// 约定：WriteRow 会把内容立刻写走，不会保留 vals 的引用，因此调用方可以复用切片。
type tableWriter interface {
	WriteRow(vals []string) error
}

// rowSink 是「一个文件一张表」的写入端（CSV / xlsx 各自实现）。
type rowSink interface {
	tableWriter
	Close() error // 刷出缓冲并关闭文件
}

// newRowSink 按格式创建 writer。
func newRowSink(format ExportFormat, path, sheet string, cfg ExportOptions) (rowSink, error) {
	if format == FormatXLSX {
		return newXLSXSink(path, sheet)
	}
	return newCSVSink(path, cfg)
}

// csvSink 流式写 CSV：csv.Writer -> 编码器（可选）-> 256KB bufio -> 文件。
type csvSink struct {
	file  *os.File
	buf   *bufio.Writer
	cw    *csv.Writer
	flush func() error // 编码器刷尾（GBK/UTF-16 等），可为 nil
}

func newCSVSink(path string, cfg ExportOptions) (*csvSink, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("file2db: 创建文件 %s 失败: %w", path, err)
	}
	buf := bufio.NewWriterSize(f, 256<<10)
	w, flush, err := encodeWriter(buf, cfg.Encoding, cfg.BOM)
	if err != nil {
		f.Close()
		return nil, err
	}
	cw := csv.NewWriter(w)
	cw.Comma = cfg.Delimiter
	return &csvSink{file: f, buf: buf, cw: cw, flush: flush}, nil
}

// WriteRow 写入一行；encoding/csv 会立刻把内容写进下游缓冲，vals 可以复用。
func (s *csvSink) WriteRow(vals []string) error {
	return s.cw.Write(vals)
}

// Close 依次刷出 csv -> 编码器 -> bufio，最后关闭文件，并把所有错误一起返回。
func (s *csvSink) Close() error {
	var errs []error
	s.cw.Flush()
	if err := s.cw.Error(); err != nil {
		errs = append(errs, err)
	}
	if s.flush != nil {
		if err := s.flush(); err != nil {
			errs = append(errs, err)
		}
	}
	if err := s.buf.Flush(); err != nil {
		errs = append(errs, err)
	}
	if err := s.file.Close(); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// xlsxWorkbook 用 StreamWriter 写 Excel：每行直接写进 XML 缓冲，不把整张表留在内存。
//
// 一个实例就是一个 xlsx 文件，可以依次写多张 Sheet（单表导出只用其中一个），
// 所以「一表一文件」与「多表一文件」共用同一套写入逻辑。
type xlsxWorkbook struct {
	file  *excelize.File
	path  string
	sheet string                 // 当前 Sheet 名
	sw    *excelize.StreamWriter // 当前 Sheet 的流写入器
	row   int                    // 当前 Sheet 已写行数；StreamWriter 要求行号严格递增
	vals  []any                  // 复用同一份 []any：SetRow 会立刻写 XML，不保留引用
	used  map[string]struct{}    // 已用 Sheet 名（小写），用来避开重名
}

func newXLSXWorkbook(path string) *xlsxWorkbook {
	return &xlsxWorkbook{
		file: excelize.NewFile(),
		path: path,
		used: make(map[string]struct{}),
	}
}

// newXLSXSink 单表导出的写入端：开一个工作簿，只用第一张 Sheet。
func newXLSXSink(path, sheet string) (*xlsxWorkbook, error) {
	wb := newXLSXWorkbook(path)
	if err := wb.BeginSheet(sheet); err != nil {
		_ = wb.file.Close()
		return nil, err
	}
	return wb, nil
}

// BeginSheet 切到下一张表用的 Sheet：先刷出上一张，再新建（或改名）工作表。
// 新 xlsx 自带的 Sheet1 会被第一张表复用，不会多出一个空 Sheet。
func (wb *xlsxWorkbook) BeginSheet(table string) error {
	if wb.sw != nil {
		if err := wb.sw.Flush(); err != nil {
			return fmt.Errorf("file2db: 刷出工作表 %q 失败: %w", wb.sheet, err)
		}
		wb.sw = nil
	}

	name := sheetName(table)
	switch {
	case len(wb.used) == 0 && strings.EqualFold(name, "Sheet1"):
		// 第一张表正好叫 Sheet1，直接用自带的
	case len(wb.used) == 0:
		if err := wb.file.SetSheetName("Sheet1", name); err != nil {
			return fmt.Errorf("file2db: 重命名首个工作表为 %q 失败: %w", name, err)
		}
	default:
		name = wb.uniqueSheetName(name)
		if _, err := wb.file.NewSheet(name); err != nil {
			return fmt.Errorf("file2db: 新建工作表 %q 失败: %w", name, err)
		}
	}
	wb.used[strings.ToLower(name)] = struct{}{}

	sw, err := wb.file.NewStreamWriter(name)
	if err != nil {
		return fmt.Errorf("file2db: 创建工作表流写入器失败: %w", err)
	}
	wb.sw, wb.sheet, wb.row = sw, name, 0
	return nil
}

// uniqueSheetName 生成工作簿内唯一且合法的 Sheet 名（Excel 限制 31 字符、不区分大小写）。
func (wb *xlsxWorkbook) uniqueSheetName(base string) string {
	name := base
	for i := 2; ; i++ {
		if _, dup := wb.used[strings.ToLower(name)]; !dup {
			return name
		}
		suffix := "_" + strconv.Itoa(i)
		name = truncateRunes(base, 31-len(suffix)) + suffix
	}
}

func (wb *xlsxWorkbook) WriteRow(vals []string) error {
	if cap(wb.vals) < len(vals) {
		wb.vals = make([]any, len(vals))
	}
	wb.vals = wb.vals[:len(vals)]
	for i, v := range vals {
		// 统一按字符串写出：数字、以 0 开头的小区编号都不会被 Excel 改格式
		wb.vals[i] = v
	}
	wb.row++
	cell, err := excelize.CoordinatesToCellName(1, wb.row)
	if err != nil {
		return err
	}
	return wb.sw.SetRow(cell, wb.vals)
}

// Close 刷出最后一张 Sheet，保存文件并释放临时资源。
func (wb *xlsxWorkbook) Close() error {
	if wb.sw != nil {
		if err := wb.sw.Flush(); err != nil {
			_ = wb.file.Close()
			return fmt.Errorf("file2db: 刷出工作表 %q 失败: %w", wb.sheet, err)
		}
		wb.sw = nil
	}
	if err := wb.file.SaveAs(wb.path); err != nil {
		_ = wb.file.Close()
		return fmt.Errorf("file2db: 保存 %s 失败: %w", wb.path, err)
	}
	return wb.file.Close()
}

// ---------- 辅助函数 ----------

// resolveExportFormat 返回最终格式：显式指定优先，否则按路径后缀推断，最后回落 CSV。
func resolveExportFormat(f ExportFormat, path string) ExportFormat {
	switch f {
	case FormatCSV, FormatXLSX:
		return f
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".xlsx", ".xlsm":
		return FormatXLSX
	}
	return FormatCSV
}

// tableColumn 描述表的一列。
type tableColumn struct {
	Name string
	PK   bool
}

// tableInfo 读取列名与主键标记（顺序与 SELECT * 一致）。
// 具体 SQL 由方言提供：SQLite 走 pragma_table_info，MySQL / Postgres 走 information_schema。
func tableInfo(ctx context.Context, db Database, d Dialect, table string) ([]tableColumn, error) {
	query, args := d.TableColumns(table)
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("file2db: 读取表 %q 结构失败: %w", table, err)
	}
	defer rows.Close()

	var out []tableColumn
	for rows.Next() {
		var (
			name string
			pk   int
		)
		if err := rows.Scan(&name, &pk); err != nil {
			return nil, fmt.Errorf("file2db: 解析表 %q 结构失败: %w", table, err)
		}
		out = append(out, tableColumn{Name: name, PK: pk > 0})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("file2db: 读取表 %q 结构失败: %w", table, err)
	}
	return out, nil
}

// exportColumns 返回要写出的列名；withIndex=false 时跳过主键列。
func exportColumns(cols []tableColumn, withIndex bool) []string {
	out := make([]string, 0, len(cols))
	for _, c := range cols {
		if !withIndex && c.PK {
			continue
		}
		out = append(out, c.Name)
	}
	return out
}

// encodeWriter 按编码名包装 writer，返回的 flush 必须在关闭前调用
// （把编码器里残余的字节刷出去），需要 BOM 时顺带写 BOM。
func encodeWriter(w io.Writer, name string, bom bool) (io.Writer, func() error, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", EncodingAuto, EncodingUTF8, "utf8":
		if bom {
			if _, err := w.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
				return nil, nil, err
			}
		}
		return w, nil, nil
	case EncodingGBK, "gb2312", EncodingGB18030:
		// GB18030 是 GBK 的超集，能编码的字符更多
		tw := transform.NewWriter(w, simplifiedchinese.GB18030.NewEncoder())
		return tw, tw.Close, nil
	case EncodingUTF16LE:
		tw := transform.NewWriter(w, unicode.UTF16(unicode.LittleEndian, utf16BOM(bom)).NewEncoder())
		return tw, tw.Close, nil
	case EncodingUTF16BE:
		tw := transform.NewWriter(w, unicode.UTF16(unicode.BigEndian, utf16BOM(bom)).NewEncoder())
		return tw, tw.Close, nil
	default:
		return nil, nil, fmt.Errorf("file2db: 不支持的导出编码 %q（可选 utf-8/gbk/gb18030/utf-16le/utf-16be）", name)
	}
}

// utf16BOM 把布尔开关转成 x/text 的 BOM 策略。
func utf16BOM(bom bool) unicode.BOMPolicy {
	if bom {
		return unicode.UseBOM
	}
	return unicode.IgnoreBOM
}

// safeFileName 把表名转成安全的文件名（表名里可能有 Windows 非法字符）。
func safeFileName(name string) string {
	name = strings.Map(func(r rune) rune {
		switch r {
		case '<', '>', ':', '"', '/', '\\', '|', '?', '*':
			return '_'
		}
		if r < 0x20 {
			return '_'
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if name == "" {
		return "table"
	}
	return name
}

// sheetName 把表名转成合法的 Excel 工作表名：
// 不能含 : \ / ? * [ ]，不能超过 31 个字符，不能为空。
func sheetName(name string) string {
	name = strings.Map(func(r rune) rune {
		switch r {
		case ':', '\\', '/', '?', '*', '[', ']':
			return '_'
		}
		return r
	}, name)
	name = strings.Trim(strings.TrimSpace(name), "'")
	if name == "" {
		return "Sheet1"
	}
	return truncateRunes(name, maxSheetName)
}

// maxSheetName 是 Excel 的工作表名长度上限（字符数）。
const maxSheetName = 31

// truncateRunes 按字符（rune）截断，不会把多字节字符切坏。
func truncateRunes(s string, limit int) string {
	if limit <= 0 {
		return ""
	}
	rs := []rune(s)
	if len(rs) <= limit {
		return s
	}
	return string(rs[:limit])
}
