package file2db

import (
	"context"
	"errors"
	"fmt"
	"io"
)

// SliceSource 把内存中的 [][]string 当成数据源：CSVSource / WorkBook 的「切片版」。
//
// 它和 CSV / Excel 走完全相同的写入管线（readHeaderRows 定位表头 + writeRows
// 建表 / 批量事务插入），所以表头行、跳过行、无表头模式、列名清洗、批量大小、
// 列类型、建索引等选项的用法与它们一致；差别只有三点：
//   - 数据来自内存切片，不涉及编码、分隔符、Sheet 名；
//   - 表名没有「文件名 / Sheet 名」可推导，默认 data，用 WithTable /
//     WithDefaultTable 指定；
//   - Close 是空操作，不占用文件句柄。
//
// 典型用途：把程序里算出来的表直接导入数据库，或在测试里替代真实文件。
type SliceSource struct {
	rows [][]string // 原始数据，含表头行（无表头模式时全是数据行）
	idx  int        // 下一条待读记录的下标
	cfg  readConfig // 沿用各数据源共用的读取配置
}

// 编译期断言：内存切片也实现了 Source。
var _ Source = (*SliceSource)(nil)

// NewSlice 从二维字符串切片创建数据源，第一行默认是表头（WithNoHeader 可改）。
//
//	src := file2db.NewSlice([][]string{
//		{"name", "age"},
//		{"张三", "100"},
//		{"李四", "101"},
//	})
//	defer src.Close() // 空操作，交给 Import/CloseAll 统一管理也行
//
//	res, err := src.Import(ctx, db, file2db.WithTable("sheet"))
//
// 行长度不要求一致：缺列补空串、多列截断，与 CSV 行尾缺列的行为相同。
// 传入的切片不会被修改，也不会被复制；导入期间请不要改写它的内容。
func NewSlice(rows [][]string, opts ...Option) *SliceSource {
	cfg := defaultReadConfig()
	for _, o := range opts {
		o(&cfg)
	}
	return &SliceSource{rows: rows, cfg: cfg}
}

// Import 把切片流式导入数据库（自动建表 + 批量事务）。ctx 控制超时与取消。
//
// 表名取 WithTable，其次 WithDefaultTable，最后兜底 data；
// 切片为空（没有任何非空行）时返回错误。
// 同一个 SliceSource 可以重复 Import，每次都会从头开始读。
func (s *SliceSource) Import(ctx context.Context, db Database, opts ...ImportOption) (*Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if db == nil {
		return nil, errors.New("file2db: db 不能为 nil")
	}

	cfg := defaultOptions()
	for _, o := range opts {
		o(&cfg)
	}

	s.Reset() // 允许同一份切片多次导入，每次都从头开始
	res, err := s.importSlice(ctx, db, cfg)
	if err != nil {
		return nil, fmt.Errorf("file2db: 导入内存切片失败: %w", err)
	}
	if res == nil {
		return nil, errors.New("file2db: 切片中没有可导入的数据")
	}
	return res, nil
}

// importSlice 导入单个切片：表头定位、建表、批量事务插入。
// 返回 nil, nil 表示切片的行都为空（读不到任何非空行），调用方应报错。
func (s *SliceSource) importSlice(ctx context.Context, db Database, cfg ImpOptions) (*Result, error) {
	rawHeader, pending, err := readHeaderRows(s.nextRaw, s.cfg)
	if err != nil {
		return nil, err
	}
	if rawHeader == nil {
		return nil, nil // 空切片 / 全是空行
	}

	// 切片没有 Sheet 名和文件名，表名只能靠选项指定，兜底为 data
	table := cfg.Table
	if table == "" {
		table = cfg.defaultTable
	}
	table = SanitizeTableName(table)

	// 默认把列名清洗成小写下划线；WithRawHeader() 时原样保留表头文字
	headers := CleanHeaders(rawHeader, s.cfg.KeepRawHeader)
	return writeRows(ctx, db, pickDialect(cfg.Dialect, db), fmt.Sprintf("内存切片 %q", table), table, headers, cfg, s.nextRaw, pending)
}

// nextRaw 返回下一条记录，读完返回 io.EOF；它就是通用的 rowReader，
// 与 CSV / Excel 的读取函数签名一致，可直接交给 readHeaderRows / writeRows。
func (s *SliceSource) nextRaw() ([]string, error) {
	if s.idx >= len(s.rows) {
		return nil, io.EOF
	}
	row := s.rows[s.idx]
	s.idx++
	return row, nil
}

// Reset 把读取位置拨回开头，便于同一份切片重复导入。
func (s *SliceSource) Reset() { s.idx = 0 }

// Len 返回切片的总行数（含表头行）。
func (s *SliceSource) Len() int { return len(s.rows) }

// Close 实现 io.Closer；切片不持有任何资源，这里什么也不做。
func (s *SliceSource) Close() error { return nil }

// ---------- 导出到 [][]string ----------

// ExportSlice 把表读进内存的 [][]string：第 0 行是列名，之后是数据行。
// 列名与 ExportTable 写出的表头完全一致（默认含自增主键 id）。
//
// 用法和 ExportTable 一样，只是产物不落盘：
//
//	rows, err := file2db.ExportSlice(ctx, db, "data", file2db.WithExportIndex(false))
//	// rows[0] 是列名，只要数据行用 rows[1:]
//
// 说明：
//   - WithExportIndex / WithExportDialect / WithExportLogger / WithExportProgress 生效，
//     与文件导出共用同一套查询与逐行写出逻辑；
//   - 导出格式、分隔符、编码、单文件等选项对内存切片没有意义，会被忽略；
//   - 和「流式写文件」不同，这里必须把整张表放进内存，表很大时请直接用 ExportTable；
//   - 空表返回只有一行表头的切片，与导出成 CSV 只写表头的行为一致。
func ExportSlice(ctx context.Context, db Database, table string, opts ...ExportOption) ([][]string, error) {
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

	d := pickDialect(cfg.Dialect, db)
	cols, err := exportColumnsOf(ctx, db, d, table, cfg)
	if err != nil {
		return nil, err
	}

	// 复用导出管线：表头定位、列清单查询、逐行转换都不重写，
	// 只把写入端从「文件 / 工作簿」换成「内存切片」。
	sink := &sliceSink{}
	if _, err := writeTableRows(ctx, sink, db, d, table, cols, cfg); err != nil {
		return nil, err
	}
	return sink.rows, nil
}

// sliceSink 是写进内存的导出端，实现 tableWriter。
//
// 它不需要 Close：没有缓冲要刷、也没有文件要关。
// 注意上游 writeTableRows 会复用同一个 []string 缓冲，所以 WriteRow 必须自己拷贝。
type sliceSink struct {
	rows [][]string
}

// 编译期断言：sliceSink 满足导出管线的写入端约定。
var _ tableWriter = (*sliceSink)(nil)

// WriteRow 把一行拷贝进内存；传入的切片可以立刻被上游复用。
func (s *sliceSink) WriteRow(vals []string) error {
	row := make([]string, len(vals))
	copy(row, vals)
	s.rows = append(s.rows, row)
	return nil
}
