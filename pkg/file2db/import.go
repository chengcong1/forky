package file2db

import (
	"log"
	"time"
)

// Result 描述一次导入的结果。
type Result struct {
	Table   string        // 实际写入的表名（只有一张表时等于 Tables[0]，多表时为空）
	Tables  []string      // 本次导入写入的所有表名（多工作表时会多于一个）
	Columns []string      // 清洗后的列名（多表时为最后一张表的列名）
	Rows    int64         // 成功写入的总行数
	Skipped int64         // 跳过的空行总数
	Indexes []string      // 导入完成后建好的索引名（WithIndex 系列选项）
	Elapsed time.Duration // 耗时
}

// IndexSpec 描述一个要在导入完成后创建的索引。
type IndexSpec struct {
	Name    string   // 索引名，留空时自动生成 idx_<表名>_<列名...>
	Columns []string // 索引列（按顺序），写原始表头或清洗后的列名都可以
	Unique  bool     // 是否唯一索引
}

// Options 导入配置。
type ImpOptions struct {
	Table         string            // 目标表名，留空时用 Sheet 名 / 默认表名
	BatchSize     int               // 每条事务包含的行数
	CreateTable   bool              // 是否自动建表
	Replace       bool              // 导入前是否 DROP TABLE
	ColumnTypes   map[string]string // 指定列类型，如 {"id": "INTEGER"}
	DefaultType   string            // 未指定列的默认类型
	KeepBlankRows bool              // 是否保留全空行
	Indexes       []IndexSpec       // 导入完成后要建的索引
	Dialect       Dialect           // 目标库方言，留空时从数据源推断
	Logger        *log.Logger       // 进度日志，nil 表示不输出
	ProgressEvery int64             // 每多少行输出一次进度，0 关闭

	defaultTable string // 内部使用：由文件名推导的默认表名
}

// ImportOption 用于配置导入行为。
type ImportOption func(*ImpOptions)

// WithTable 指定目标表名（默认：Excel 用 Sheet 名，CSV 用文件名）。
func WithTable(name string) ImportOption {
	return func(o *ImpOptions) { o.Table = name }
}

// WithDefaultTable 指定表名的兜底值：
// 当既没有 WithTable、数据源也没有 Sheet 名（如 CSV）时使用。
func WithDefaultTable(name string) ImportOption {
	return func(o *ImpOptions) { o.defaultTable = name }
}

// WithBatchSize 设置每条事务写入的行数，默认 500。
func WithBatchSize(n int) ImportOption {
	return func(o *ImpOptions) {
		if n > 0 {
			o.BatchSize = n
		}
	}
}

// WithCreateTable 指定是否自动建表，默认 true。
func WithCreateTable(b bool) ImportOption {
	return func(o *ImpOptions) { o.CreateTable = b }
}

// WithReplace 导入前先删除同名表（等价于全量覆盖）。
func WithReplace() ImportOption {
	return func(o *ImpOptions) { o.Replace = true }
}

// WithColumnTypes 指定列类型，例如 map[string]string{"id": "INTEGER"}，
// 未指定的列使用默认类型。
func WithColumnTypes(types map[string]string) ImportOption {
	return func(o *ImpOptions) { o.ColumnTypes = types }
}

// WithDefaultColumnType 设置未指定列的默认类型，默认 TEXT（保留原始文本，最安全）。
func WithDefaultColumnType(t string) ImportOption {
	return func(o *ImpOptions) {
		if t != "" {
			o.DefaultType = t
		}
	}
}

// WithImportDialect 显式指定目标库方言。
// 用 Open 返回的 *DB（或 Imports）时不需要它；只有拿裸 *sql.DB 导 MySQL/Postgres 时才要。
func WithImportDialect(d Dialect) ImportOption {
	return func(o *ImpOptions) { o.Dialect = d }
}

// WithIndex 导入写完后为这些列建索引（按传入顺序组成一个复合索引）。
// 可以多次调用，调用几次就建几个索引。
//
// 放在写完之后建是刻意的：数据库一次性建索引比逐行维护快得多。
//
// 列名写原始表头（"AGE"）或清洗后的列名（"age"）都能对上；
// 多 Sheet 导入时，哪张表没这列就跳过哪张表的索引。
//
//	file2db.WithIndex("name"), file2db.WithIndex("name", "age")
func WithIndex(columns ...string) ImportOption {
	return appendIndex(IndexSpec{Columns: columns})
}

// WithUniqueIndex 同 WithIndex，但建唯一索引；数据里有重复值时导入会报错。
func WithUniqueIndex(columns ...string) ImportOption {
	return appendIndex(IndexSpec{Columns: columns, Unique: true})
}

// WithIndexSpec 用完整描述建索引，可自定义索引名与唯一性。
func WithIndexSpec(spec IndexSpec) ImportOption {
	return appendIndex(spec)
}

// appendIndex 把索引描述追加到配置里（WithIndex 可多次调用）。
func appendIndex(spec IndexSpec) ImportOption {
	return func(o *ImpOptions) {
		if len(spec.Columns) > 0 {
			o.Indexes = append(o.Indexes, spec)
		}
	}
}

// WithKeepBlankRows 保留全空行（默认跳过）。
func WithKeepBlankRows() ImportOption {
	return func(o *ImpOptions) { o.KeepBlankRows = true }
}

// WithLogger 输出导入进度日志。
func WithLogger(l *log.Logger) ImportOption {
	return func(o *ImpOptions) { o.Logger = l }
}

// WithProgress 每 every 行输出一次进度。
func WithProgress(every int64) ImportOption {
	return func(o *ImpOptions) {
		if every > 0 {
			o.ProgressEvery = every
		}
	}
}

func defaultOptions() ImpOptions {
	return ImpOptions{
		BatchSize:   500,
		CreateTable: true,
		DefaultType: "TEXT",
	}
}
