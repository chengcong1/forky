package main

import (
	"bufio"
	"context"
	"database/sql"
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

	"github.com/chengcong1/forky/pkg/file2db"

	"github.com/xuri/excelize/v2"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

// 输出格式常量。
const (
	formatCSV   = "csv"
	formatXLSX  = "xlsx"
	formatTable = "table" // 控制台对齐打印，只用于 stdout
)

// logger 是给 file2db 打印进度用的；-quiet 时传 nil（库会自动跳过进度日志）。
var logger = log.New(os.Stderr, "", log.LstdFlags)

// progressLogger 返回当前该用的进度日志器。
func progressLogger(cfg *Config) *log.Logger {
	if cfg.Quiet {
		return nil
	}
	return logger
}

// openDB 打开数据库。默认 SQLite + data.db，换库只改 -driver / -dsn
// （mysql、postgres 驱动已在本包空导入，可直接用）。
func openDB(cfg *Config) (*file2db.DB, error) {
	opts := make([]file2db.DBOption, 0, 2)
	if cfg.DB.MaxOpenConns > 0 {
		opts = append(opts, file2db.WithMaxOpenConns(cfg.DB.MaxOpenConns))
	}
	if len(cfg.DB.Setup) > 0 {
		opts = append(opts, file2db.WithSetup(cfg.DB.Setup...))
	}
	return file2db.Open(cfg.DB.Driver, cfg.DB.DSN, opts...)
}

// ---------- import：文件 -> 数据库 ----------

// runImport 把配置里给出的文件依次导入数据库。
//
// 表名规则沿用 file2db：Excel 一个 Sheet 一张表（默认取 Sheet 名），
// CSV/TSV 一个文件一张表（默认取文件名）。
// ctx 决定超时与 Ctrl+C 能否中断（中断时当前批次回滚、已提交批次保留）。
func runImport(ctx context.Context, cfg *Config, db *file2db.DB) error {
	if len(cfg.Import.Files) == 0 {
		return errors.New("import：没有要导入的文件，用 -file 指定，或直接写在命令末尾")
	}

	readOpts, err := buildReadOptions(cfg.Import)
	if err != nil {
		return err
	}
	impOpts := buildImportOptions(cfg.Import, progressLogger(cfg))

	start := time.Now()
	results, err := file2db.Import(ctx, db, cfg.Import.Files, readOpts, impOpts)
	// 中途失败时前面成功的表已经落库，先把结果打出来再返回错误
	for _, r := range results {
		reportImport(r)
	}
	if err != nil {
		return err
	}
	if len(results) > 0 {
		var rows int64
		for _, r := range results {
			rows += r.Rows
		}
		fmt.Printf("导入完成：%d 个文件 / %d 行，总耗时 %s\n", len(results), rows, time.Since(start))
	}
	return nil
}

// reportImport 打印一个文件（工作簿）的导入结果。
func reportImport(r *file2db.Result) {
	if r == nil {
		return
	}
	switch {
	case len(r.Tables) > 1:
		fmt.Printf("导入完成：%d 张表 { %s }，共 %d 行（跳过 %d 行），耗时 %s\n",
			len(r.Tables), strings.Join(r.Tables, ", "), r.Rows, r.Skipped, r.Elapsed)
	case r.Table != "":
		fmt.Printf("导入完成：%s，%d 行（跳过 %d 行），耗时 %s\n", r.Table, r.Rows, r.Skipped, r.Elapsed)
	default:
		fmt.Println("导入完成：没有可导入的工作表")
	}
	if len(r.Indexes) > 0 {
		fmt.Printf("  已建索引：%s\n", strings.Join(r.Indexes, ", "))
	}
}

// buildReadOptions 把读取配置翻译成 file2db 的读取选项。
func buildReadOptions(ic ImportConfig) ([]file2db.Option, error) {
	opts := make([]file2db.Option, 0, 16)
	if ic.HeaderRow > 0 {
		opts = append(opts, file2db.WithHeaderRow(ic.HeaderRow))
	}
	if ic.SkipRows > 0 {
		opts = append(opts, file2db.WithSkipRows(ic.SkipRows))
	}
	if ic.NoHeader {
		opts = append(opts, file2db.WithNoHeader())
	}
	if ic.RawHeader {
		opts = append(opts, file2db.WithRawHeader())
	}
	if len(ic.Sheets) > 0 {
		opts = append(opts, file2db.WithSheets(ic.Sheets))
	}
	if len(ic.SkipSheets) > 0 {
		opts = append(opts, file2db.WithSkipSheets(ic.SkipSheets))
	}
	opts = append(opts,
		file2db.WithOnlyActiveSheet(ic.OnlyActiveSheet),
		file2db.WithRawCellValue(ic.RawCellValue),
		file2db.WithLazyQuotes(ic.LazyQuotes),
		file2db.WithTrimLeadingSpace(ic.TrimLeadingSpace),
		file2db.WithFieldsPerRecord(ic.FieldsPerRecord),
	)
	if ic.Encoding != "" {
		opts = append(opts, file2db.WithEncoding(ic.Encoding))
	}
	if ic.Delimiter != "" {
		r, err := parseDelimiter(ic.Delimiter)
		if err != nil {
			return nil, fmt.Errorf("import：%w", err)
		}
		opts = append(opts, file2db.WithDelimiter(r))
	}
	if ic.Comment != "" {
		rs := []rune(strings.TrimSpace(ic.Comment))
		if len(rs) != 1 {
			return nil, fmt.Errorf("import：注释字符只能是单个字符，收到 %q", ic.Comment)
		}
		opts = append(opts, file2db.WithComment(rs[0]))
	}
	return opts, nil
}

// buildImportOptions 把写入配置翻译成 file2db 的导入选项。
func buildImportOptions(ic ImportConfig, lg *log.Logger) []file2db.ImportOption {
	opts := []file2db.ImportOption{file2db.WithCreateTable(ic.CreateTable)}
	if ic.Table != "" {
		opts = append(opts, file2db.WithTable(ic.Table))
	}
	if ic.Replace {
		opts = append(opts, file2db.WithReplace())
	}
	if ic.BatchSize > 0 {
		opts = append(opts, file2db.WithBatchSize(ic.BatchSize))
	}
	if ic.DefaultType != "" {
		opts = append(opts, file2db.WithDefaultColumnType(ic.DefaultType))
	}
	if len(ic.ColumnTypes) > 0 {
		opts = append(opts, file2db.WithColumnTypes(ic.ColumnTypes))
	}
	if ic.KeepBlankRows {
		opts = append(opts, file2db.WithKeepBlankRows())
	}
	for _, idx := range ic.Indexes {
		if len(idx.Columns) == 0 {
			continue
		}
		opts = append(opts, file2db.WithIndexSpec(file2db.IndexSpec{
			Name:    idx.Name,
			Columns: idx.Columns,
			Unique:  idx.Unique,
		}))
	}
	// 日志器为 nil 时库内部会跳过进度输出，正好对应 -quiet
	return append(opts, file2db.WithLogger(lg), file2db.WithProgress(ic.Progress))
}

// ---------- export：数据库 -> 文件 ----------

// runExport 把库里的表导出成 CSV / XLSX。
// 默认一表一个文件放在 -out 目录里；给了 -single-file 则多张表写进同一个 xlsx。
func runExport(ctx context.Context, cfg *Config, db *file2db.DB) error {
	ec := cfg.Export
	dir := ec.Dir
	if dir == "" {
		if ec.SingleFile == "" {
			return errors.New("export：请用 -out 指定输出目录，或用 -single-file 指定单个 xlsx 文件名")
		}
		dir = "." // 只给了文件名时落到当前目录
	}

	opts := []file2db.ExportOption{
		file2db.WithExportIndex(ec.Index),
		file2db.WithExportLogger(progressLogger(cfg)),
		file2db.WithExportProgress(ec.Progress),
	}
	if ec.SingleFile != "" {
		opts = append(opts, file2db.WithExportSingleFile(ec.SingleFile))
	}
	if len(ec.Tables) > 0 {
		opts = append(opts, file2db.WithExportTables(ec.Tables...))
	}
	if ec.Format != "" {
		format, err := parseFormat(ec.Format)
		if err != nil {
			return fmt.Errorf("export：%w", err)
		}
		opts = append(opts, file2db.WithExportFormat(format))
	}
	if ec.Encoding != "" {
		opts = append(opts, file2db.WithExportEncoding(ec.Encoding))
	}
	if ec.BOM {
		opts = append(opts, file2db.WithExportBOM())
	}
	if ec.Delimiter != "" {
		r, err := parseDelimiter(ec.Delimiter)
		if err != nil {
			return fmt.Errorf("export：%w", err)
		}
		opts = append(opts, file2db.WithExportDelimiter(r))
	}

	start := time.Now()
	results, err := file2db.ExportTables(ctx, db, dir, opts...)
	var rows int64
	for _, r := range results {
		rows += r.Rows
		sheet := ""
		if r.Sheet != "" {
			sheet = " [" + r.Sheet + "]"
		}
		fmt.Printf("导出完成：%s -> %s%s，%d 行，耗时 %s\n", r.Table, r.Path, sheet, r.Rows, r.Elapsed)
	}
	if err != nil {
		return fmt.Errorf("export：%w", err)
	}
	fmt.Printf("导出完成：%d 张表 / %d 行，总耗时 %s\n", len(results), rows, time.Since(start))
	return nil
}

// parseFormat 校验导出格式（query 的 table 格式不在这里处理）。
func parseFormat(name string) (file2db.ExportFormat, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case string(file2db.FormatCSV):
		return file2db.FormatCSV, nil
	case string(file2db.FormatXLSX):
		return file2db.FormatXLSX, nil
	default:
		return "", fmt.Errorf("不支持的格式 %q（可选 csv / xlsx）", name)
	}
}

// ---------- query：执行 SQL ----------

// runQueries 执行查询。命令行给了 -sql / -sql-file / 位置参数时就只跑这一条，
// 否则跑配置文件 queries 里的每一条（顺序执行）。
func runQueries(ctx context.Context, cfg *Config, db *file2db.DB) error {
	queries := cfg.Queries
	if cfg.cli.sqlProvided {
		// 命令行 SQL 与配置文件里的查询混在一起会分不清谁输出到哪，所以直接互斥
		queries = []QueryConfig{cfg.cliQuery()}
	}
	if len(queries) == 0 {
		return errors.New(`query：请用 -sql "SELECT ..." / -sql-file，或在配置文件的 queries 里写 SQL`)
	}
	for _, q := range queries {
		if err := runQuery(ctx, db, cfg, q); err != nil {
			return err
		}
	}
	return nil
}

// cliQuery 把命令行里的 SQL 与输出设置组装成一条查询配置。
// 格式留空时按 -sql-out 的后缀推断，所以这里只有显式给了 -format 才带上。
func (c *Config) cliQuery() QueryConfig {
	format := ""
	if c.cli.formatSet {
		format = c.cli.format
	}
	return QueryConfig{
		Name:      "命令行 SQL",
		SQL:       c.cli.sql,
		SQLFile:   c.cli.sqlFile,
		Out:       c.cli.sqlOut,
		Sheet:     c.cli.sqlSheet,
		NoHeader:  c.cli.sqlNoHeader,
		Format:    format,
		Encoding:  c.Export.Encoding,
		Delimiter: c.Export.Delimiter,
		BOM:       c.Export.BOM,
	}
}

// runQuery 执行一条 SQL：非查询语句走 Exec 打印影响行数，查询语句按目标格式写出结果。
// 两条路径都用带 ctx 的版本，长 SQL 或大结果集能被 -timeout / Ctrl+C 打断。
func runQuery(ctx context.Context, db *file2db.DB, cfg *Config, q QueryConfig) error {
	stmt, err := resolveSQL(cfg.cli.config, q)
	if err != nil {
		return err
	}
	if stmt == "" {
		return errors.New("query：SQL 为空")
	}
	label := q.Name
	if label == "" {
		label = "SQL"
	}

	if !isQueryStatement(stmt) {
		start := time.Now()
		res, err := db.ExecContext(ctx, stmt)
		if err != nil {
			return fmt.Errorf("%s：执行失败: %w", label, err)
		}
		n, _ := res.RowsAffected()
		fmt.Printf("%s：执行完成，影响 %d 行，耗时 %s\n", label, n, time.Since(start))
		return nil
	}

	rows, err := db.QueryContext(ctx, stmt)
	if err != nil {
		return fmt.Errorf("%s：查询失败: %w", label, err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return fmt.Errorf("%s：读取结果列失败: %w", label, err)
	}
	dest, err := resolveDestination(q)
	if err != nil {
		return fmt.Errorf("%s：%w", label, err)
	}

	start := time.Now()
	n, err := writeResult(dest, cols, rowScanner(rows, len(cols)))
	if err != nil {
		return fmt.Errorf("%s：%w", label, err)
	}
	fmt.Printf("%s：%d 行 -> %s，耗时 %s\n", label, n, dest.describe(), time.Since(start))
	return nil
}

// resolveSQL 返回要执行的 SQL：内联 sql 优先，其次 sqlFile
// （相对路径按配置文件所在目录解析，配置文件与 SQL 文件可以放在一起）。
func resolveSQL(configPath string, q QueryConfig) (string, error) {
	if text := strings.TrimSpace(q.SQL); text != "" {
		return text, nil
	}
	path := strings.TrimSpace(q.SQLFile)
	if path == "" {
		return "", nil
	}
	if !filepath.IsAbs(path) && configPath != "" {
		path = filepath.Join(filepath.Dir(configPath), path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("读取 SQL 文件失败: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

// isQueryStatement 判断语句是否会返回结果集：会返回的走 Query，
// 其余（INSERT/UPDATE/DELETE/DDL）走 Exec 拿影响行数。
func isQueryStatement(stmt string) bool {
	switch firstKeyword(stmt) {
	case "SELECT", "WITH", "VALUES", "PRAGMA", "EXPLAIN", "SHOW", "DESCRIBE", "DESC", "TABLE":
		return true
	}
	return false
}

// firstKeyword 取语句的首个关键字（跳过前导注释、空白与括号），统一大写。
func firstKeyword(stmt string) string {
	s := strings.TrimSpace(stmt)
	for {
		switch {
		case strings.HasPrefix(s, "--"):
			i := strings.IndexAny(s, "\r\n")
			if i < 0 {
				return ""
			}
			s = strings.TrimSpace(s[i+1:])
			continue
		case strings.HasPrefix(s, "/*"):
			i := strings.Index(s, "*/")
			if i < 0 {
				return ""
			}
			s = strings.TrimSpace(s[i+2:])
			continue
		case strings.HasPrefix(s, "("):
			s = strings.TrimSpace(s[1:])
			continue
		}
		break
	}
	end := strings.IndexFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '_')
	})
	if end < 0 {
		end = len(s)
	}
	return strings.ToUpper(s[:end])
}

// ---------- tables：列出表与行数 ----------

// runTables 列出库里的用户表与行数；输出目标与 query 共用（-sql-out / -format ...）。
func runTables(ctx context.Context, cfg *Config, db *file2db.DB) error {
	tables, err := file2db.Tables(ctx, db)
	if err != nil {
		return fmt.Errorf("读取表清单失败: %w", err)
	}
	if len(tables) == 0 {
		fmt.Println("库里没有用户表")
		return nil
	}

	rows := make([][]string, 0, len(tables))
	for _, t := range tables {
		n, err := file2db.CountRows(ctx, db, t)
		if err != nil {
			return fmt.Errorf("统计表 %s 行数失败: %w", t, err)
		}
		rows = append(rows, []string{t, strconv.FormatInt(n, 10)})
	}

	dest, err := resolveDestination(cfg.cliQuery())
	if err != nil {
		return err
	}
	if _, err := writeResult(dest, []string{"table", "rows"}, sliceRows(rows)); err != nil {
		return err
	}
	if dest.path != "" {
		fmt.Printf("共 %d 张表 -> %s\n", len(tables), dest.path)
	}
	return nil
}

// ---------- 输出端 ----------

// destination 描述结果写到哪里、用什么格式。
type destination struct {
	path      string // 空 = 控制台
	format    string
	sheet     string // xlsx 的 Sheet 名
	encoding  string
	delimiter rune
	bom       bool
	noHeader  bool
}

// describe 返回一句人话，用于日志里说明输出去向。
func (d destination) describe() string {
	if d.path == "" {
		if d.format == formatTable {
			return "控制台（对齐打印）"
		}
		return "控制台（" + d.format + "）"
	}
	return d.path + "（" + d.format + "）"
}

// resolveDestination 决定输出格式：显式指定优先；留空时按输出文件后缀推断，
// 写控制台则用对齐打印（table）。
func resolveDestination(q QueryConfig) (destination, error) {
	d := destination{
		path:     strings.TrimSpace(q.Out),
		sheet:    q.Sheet,
		encoding: q.Encoding,
		bom:      q.BOM,
		noHeader: q.NoHeader,
	}

	format := strings.ToLower(strings.TrimSpace(q.Format))
	if format == "" {
		switch {
		case d.path == "":
			format = formatTable
		case isXLSXPath(d.path):
			format = formatXLSX
		default:
			format = formatCSV
		}
	}
	switch format {
	case formatCSV, formatXLSX, formatTable:
	default:
		return destination{}, fmt.Errorf("不支持的输出格式 %q（可选 csv / xlsx / table；table 只用于控制台）", q.Format)
	}
	if format == formatTable && d.path != "" {
		return destination{}, errors.New("format=table 只用于控制台；写文件请用 csv 或 xlsx")
	}
	d.format = format

	if d.encoding == "" {
		d.encoding = file2db.EncodingUTF8
	}
	delim := strings.TrimSpace(q.Delimiter)
	if delim == "" {
		delim = ","
	}
	r, err := parseDelimiter(delim)
	if err != nil {
		return destination{}, err
	}
	d.delimiter = r
	return d, nil
}

// isXLSXPath 判断输出路径是不是 Excel 文件。
func isXLSXPath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".xlsx", ".xlsm":
		return true
	}
	return false
}

// writeResult 把结果写到目标，返回写出的数据行数（不含表头）。
// next 每次返回一行，读完返回 io.EOF；返回的切片会被复用，调用方不能长期持有。
func writeResult(dest destination, cols []string, next func() ([]string, error)) (int64, error) {
	if dest.format == formatTable {
		return writeConsoleTable(dest, cols, next)
	}
	if dest.path != "" {
		// 输出路径里的目录可能还不存在（比如 -sql-out out/结果.csv），先建好
		if dir := filepath.Dir(dest.path); dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return 0, fmt.Errorf("创建输出目录 %s 失败: %w", dir, err)
			}
		}
	}

	sink, err := newFileSink(dest)
	if err != nil {
		return 0, err
	}
	ok := false
	defer func() {
		if !ok { // 中途失败不留半截文件
			_ = sink.Close()
			if dest.path != "" {
				_ = os.Remove(dest.path)
			}
		}
	}()

	if !dest.noHeader {
		if err := sink.WriteRow(cols); err != nil {
			return 0, fmt.Errorf("写表头失败: %w", err)
		}
	}
	var n int64
	for {
		vals, err := next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return n, err
		}
		if err := sink.WriteRow(vals); err != nil {
			return n, fmt.Errorf("写第 %d 行失败: %w", n+1, err)
		}
		n++
	}
	if err := sink.Close(); err != nil {
		return n, err
	}
	ok = true
	return n, nil
}

// resultSink 是结果的输出端：只逐行写，文件 / 工作簿 / 控制台的细节各自实现。
// 约定 WriteRow 会立刻把内容写走，不保留传入切片的引用。
type resultSink interface {
	WriteRow(vals []string) error
	Close() error
}

// newFileSink 按格式创建落盘的输出端。
func newFileSink(dest destination) (resultSink, error) {
	if dest.format == formatXLSX {
		return newXLSXSink(dest)
	}
	return newCSVSink(dest)
}

// csvSink 流式写 CSV：encoding/csv -> 可选编码器 -> 256KB bufio -> 文件或 stdout。
type csvSink struct {
	file  *os.File // 写 stdout 时为空
	buf   *bufio.Writer
	cw    *csv.Writer
	flush func() error // 编码器刷尾，可为 nil
}

func newCSVSink(dest destination) (*csvSink, error) {
	var f *os.File
	var dst io.Writer = os.Stdout
	if dest.path != "" {
		var err error
		f, err = os.Create(dest.path)
		if err != nil {
			return nil, fmt.Errorf("创建文件 %s 失败: %w", dest.path, err)
		}
		dst = f
	}
	buf := bufio.NewWriterSize(dst, 256<<10)
	w, flush, err := encodeWriter(buf, dest.encoding, dest.bom)
	if err != nil {
		if f != nil {
			_ = f.Close()
		}
		return nil, err
	}
	cw := csv.NewWriter(w)
	cw.Comma = dest.delimiter
	return &csvSink{file: f, buf: buf, cw: cw, flush: flush}, nil
}

// WriteRow 写一行；encoding/csv 会立刻写进下游缓冲，vals 可以复用。
func (s *csvSink) WriteRow(vals []string) error { return s.cw.Write(vals) }

// Close 依次刷出 csv -> 编码器 -> bufio，最后关文件，并把错误一起返回。
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
	if s.file != nil {
		if err := s.file.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// xlsxSink 用 excelize 的 StreamWriter 写 xlsx：逐行落盘，内存占用与结果集大小无关。
type xlsxSink struct {
	file  *excelize.File
	path  string
	sheet string
	sw    *excelize.StreamWriter
	row   int   // 已写行数；StreamWriter 要求行号严格递增
	vals  []any // 复用同一份 []any
}

func newXLSXSink(dest destination) (*xlsxSink, error) {
	sheet := sheetName(dest.sheet)
	f := excelize.NewFile()
	if !strings.EqualFold(sheet, "Sheet1") {
		if err := f.SetSheetName("Sheet1", sheet); err != nil {
			_ = f.Close()
			return nil, fmt.Errorf("重命名工作表失败: %w", err)
		}
	}
	sw, err := f.NewStreamWriter(sheet)
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("创建工作表流写入器失败: %w", err)
	}
	return &xlsxSink{file: f, path: dest.path, sheet: sheet, sw: sw}, nil
}

// WriteRow 统一按字符串写出：数字、以 0 开头的小区编号都不会被 Excel 改格式，
// 与 file2db 导出的行为保持一致。
func (s *xlsxSink) WriteRow(vals []string) error {
	if cap(s.vals) < len(vals) {
		s.vals = make([]any, len(vals))
	}
	s.vals = s.vals[:len(vals)]
	for i, v := range vals {
		s.vals[i] = v
	}
	s.row++
	cell, err := excelize.CoordinatesToCellName(1, s.row)
	if err != nil {
		return err
	}
	return s.sw.SetRow(cell, s.vals)
}

// Close 刷出工作表、保存文件并释放临时资源。
func (s *xlsxSink) Close() error {
	if s.sw != nil {
		if err := s.sw.Flush(); err != nil {
			_ = s.file.Close()
			return fmt.Errorf("刷出工作表失败: %w", err)
		}
		s.sw = nil
	}
	if err := s.file.SaveAs(s.path); err != nil {
		_ = s.file.Close()
		return fmt.Errorf("保存 %s 失败: %w", s.path, err)
	}
	return s.file.Close()
}

// writeConsoleTable 把结果对齐打印到控制台。
//
// 为了算列宽必须把整表留在内存，所以只适合人工核对的小结果集；
// 大结果请用 -format csv（流式，可直接重定向到文件）。
func writeConsoleTable(dest destination, cols []string, next func() ([]string, error)) (int64, error) {
	rows := make([][]string, 0, 64)
	widths := make([]int, len(cols))
	for i, c := range cols {
		widths[i] = displayWidth(c)
	}
	if !dest.noHeader {
		rows = append(rows, cols)
	}

	var n int64
	for {
		vals, err := next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return n, err
		}
		row := make([]string, len(vals)) // 上游复用切片，必须自己拷一份
		copy(row, vals)
		for i, v := range row {
			if i < len(widths) {
				if w := displayWidth(v); w > widths[i] {
					widths[i] = w
				}
			}
		}
		rows = append(rows, row)
		n++
	}

	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()
	for _, row := range rows {
		for i, v := range row {
			if i > 0 {
				fmt.Fprint(out, "  ")
			}
			fmt.Fprint(out, v)
			if i < len(row)-1 && i < len(widths) {
				if pad := widths[i] - displayWidth(v); pad > 0 {
					fmt.Fprint(out, strings.Repeat(" ", pad))
				}
			}
		}
		fmt.Fprintln(out)
	}
	return n, nil
}

// rowScanner 把结果集包成「每次返回一行字符串」的闭包，读完返回 io.EOF。
// 复用同一组扫描缓冲，所以调用方不能长期持有返回的切片。
func rowScanner(rows *sql.Rows, n int) func() ([]string, error) {
	raw := make([]any, n)
	ptrs := make([]any, n)
	strs := make([]string, n)
	for i := range raw {
		ptrs[i] = &raw[i]
	}
	return func() ([]string, error) {
		if !rows.Next() {
			if err := rows.Err(); err != nil {
				return nil, fmt.Errorf("遍历结果集失败: %w", err)
			}
			return nil, io.EOF
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, fmt.Errorf("读取数据失败: %w", err)
		}
		for i, v := range raw {
			strs[i] = anyToString(v)
		}
		return strs, nil
	}
}

// sliceRows 把内存里的二维切片变成 writeResult 需要的逐行读取函数。
func sliceRows(rows [][]string) func() ([]string, error) {
	i := 0
	return func() ([]string, error) {
		if i >= len(rows) {
			return nil, io.EOF
		}
		row := rows[i]
		i++
		return row, nil
	}
}

// ---------- 小工具 ----------

// anyToString 把驱动返回的值转成要写出的字符串。
// 覆盖 SQLite 会返回的全部类型，走 strconv 快路径而不是 fmt 反射（逐单元格的热点）。
func anyToString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case []byte:
		return string(x) // BLOB / 旧驱动可能复用底层数组，必须拷一份
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

// encodeWriter 按编码名包装 writer，返回的 flush 必须在关闭前调用
// （把编码器里残余的字节刷出去）；可选的编码与 file2db 导出保持同一套。
func encodeWriter(w io.Writer, name string, bom bool) (io.Writer, func() error, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", file2db.EncodingAuto, file2db.EncodingUTF8, "utf8":
		if bom {
			if _, err := w.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
				return nil, nil, err
			}
		}
		return w, nil, nil
	case file2db.EncodingGBK, "gb2312", file2db.EncodingGB18030:
		// GB18030 是 GBK 的超集，能编码的字符更多
		tw := transform.NewWriter(w, simplifiedchinese.GB18030.NewEncoder())
		return tw, tw.Close, nil
	case file2db.EncodingUTF16LE:
		tw := transform.NewWriter(w, unicode.UTF16(unicode.LittleEndian, bomPolicy(bom)).NewEncoder())
		return tw, tw.Close, nil
	case file2db.EncodingUTF16BE:
		tw := transform.NewWriter(w, unicode.UTF16(unicode.BigEndian, bomPolicy(bom)).NewEncoder())
		return tw, tw.Close, nil
	default:
		return nil, nil, fmt.Errorf("不支持的输出编码 %q（可选 utf-8/gbk/gb18030/utf-16le/utf-16be）", name)
	}
}

// bomPolicy 把布尔开关转成 x/text 的 BOM 策略。
func bomPolicy(bom bool) unicode.BOMPolicy {
	if bom {
		return unicode.UseBOM
	}
	return unicode.IgnoreBOM
}

// sheetName 把任意文本转成合法的 Excel 工作表名（不含 : \ / ? * [ ]，最长 31 字符）。
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
	if rs := []rune(name); len(rs) > 31 {
		name = string(rs[:31])
	}
	return name
}

// displayWidth 估算字符串在等宽终端里占的列数：CJK / 全角字符按 2 列算，其余 1 列。
// 只用于控制台对齐，不追求完全精确。
func displayWidth(s string) int {
	w := 0
	for _, r := range s {
		if isWideRune(r) {
			w += 2
		} else {
			w++
		}
	}
	return w
}

// isWideRune 判断是否是常见宽字符（汉字、假名、韩文、全角符号等）。
func isWideRune(r rune) bool {
	switch {
	case r < 0x1100:
		return false
	case r >= 0x1100 && r <= 0x115F, // 韩文字母
		r >= 0x2E80 && r <= 0xA4CF && r != 0x303F, // CJK 部首、假名、汉字
		r >= 0xAC00 && r <= 0xD7A3,                // 韩文音节
		r >= 0xF900 && r <= 0xFAFF,                // CJK 兼容汉字
		r >= 0xFE30 && r <= 0xFE6F,                // CJK 兼容形式
		r >= 0xFF00 && r <= 0xFF60,                // 全角字符
		r >= 0xFFE0 && r <= 0xFFE6,
		r >= 0x20000 && r <= 0x3FFFD: // 扩展区汉字
		return true
	}
	return false
}
