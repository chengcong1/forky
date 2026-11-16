package file2db

import (
	"strconv"
	"strings"
)

// Dialect 描述各数据库的差异点：标识符引用、参数占位符、自增主键、元数据查询。
//
// 内置 SQLite / MySQL / PostgreSQL 三种（SQLite / MySQL / Postgres 三个变量），
// 需要支持别的库时实现本接口即可。
type Dialect interface {
	// Name 方言名：sqlite / mysql / postgres。
	Name() string
	// QuoteIdent 转义标识符，避免 SQL 注入与关键字冲突。
	QuoteIdent(name string) string
	// BindVar 返回第 i 个（从 0 开始）参数占位符。
	BindVar(i int) string
	// AutoIncrementPK 返回建表时自增主键列的定义（列名固定为 id）。
	AutoIncrementPK() string
	// CreateTableSuffix 返回建表语句末尾的方言尾巴（MySQL 用来指定字符集）。
	CreateTableSuffix() string
	// ListTables 返回查询全部用户表名的语句（按名字排序）。
	ListTables() string
	// TableExists 返回判断表是否存在的语句与参数。
	TableExists(table string) (string, []any)
	// TableColumns 返回查询列名与主键标记的语句与参数。
	// 结果必须正好两列 (name, 是否主键)，且按列在表中的顺序排列。
	TableColumns(table string) (string, []any)
	// IndexIfNotExists 报告是否支持 CREATE INDEX IF NOT EXISTS。
	// 不支持的方言（MySQL）要先查 IndexExists 再建。
	IndexIfNotExists() bool
	// IndexExists 返回判断索引是否存在的语句与参数，
	// 供不支持 IF NOT EXISTS 的方言建索引前查重。
	IndexExists(table, name string) (string, []any)
	// Setup 返回连接建立后需要执行的语句（SQLite 的 PRAGMA 等）。
	Setup() []string
}

// DialectForDriver 由驱动名推断方言；未知驱动按 SQLite 处理。
//
//	mysql                   -> MySQL
//	postgres / postgresql   -> Postgres
//	pgx / pgx/v5 / lib/pq   -> Postgres
//	其他                     -> SQLite
func DialectForDriver(driver string) Dialect {
	switch strings.ToLower(strings.TrimSpace(driver)) {
	case "mysql", "mariadb":
		return MySQL
	case "postgres", "postgresql", "pgx", "pgx/v4", "pgx/v5", "lib/pq", "pq":
		return Postgres
	default:
		return SQLite
	}
}

// ---------- SQLite ----------

// SQLite 方言：双引号标识符、? 占位符、INTEGER PRIMARY KEY AUTOINCREMENT。
var SQLite Dialect = sqliteDialect{}

type sqliteDialect struct{}

func (sqliteDialect) Name() string { return "sqlite" }

func (sqliteDialect) QuoteIdent(name string) string { return quoteDouble(name) }

func (sqliteDialect) BindVar(int) string { return "?" }

func (sqliteDialect) AutoIncrementPK() string { return "id INTEGER PRIMARY KEY AUTOINCREMENT" }

func (sqliteDialect) CreateTableSuffix() string { return "" }

func (sqliteDialect) ListTables() string {
	return `SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`
}

func (sqliteDialect) TableExists(table string) (string, []any) {
	return `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, []any{table}
}

func (sqliteDialect) TableColumns(table string) (string, []any) {
	// pragma_table_info 是表函数，结果正好是 (name, pk)，且支持绑定参数
	return `SELECT name, pk FROM pragma_table_info(?) ORDER BY cid`, []any{table}
}

func (sqliteDialect) IndexIfNotExists() bool { return true }

func (sqliteDialect) IndexExists(table, name string) (string, []any) {
	return `SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ? AND tbl_name = ?`,
		[]any{name, table}
}

func (sqliteDialect) Setup() []string {
	// 兼顾导入速度与安全性
	return []string{
		"PRAGMA journal_mode = WAL",   // WAL：读写并发更好
		"PRAGMA synchronous = NORMAL", // 减少 fsync，速度明显提升
		"PRAGMA cache_size = -64000",  // 64MB 页缓存
		"PRAGMA temp_store = MEMORY",  // 临时表放内存
		"PRAGMA busy_timeout = 10000", // 锁等待 10s，避免多进程导入直接报错
	}
}

// ---------- MySQL ----------

// MySQL 方言：反引号标识符、? 占位符、AUTO_INCREMENT，建表默认 utf8mb4 以免中文乱码。
var MySQL Dialect = mysqlDialect{}

type mysqlDialect struct{}

func (mysqlDialect) Name() string { return "mysql" }

func (mysqlDialect) QuoteIdent(name string) string { return quoteBacktick(name) }

func (mysqlDialect) BindVar(int) string { return "?" }

func (mysqlDialect) AutoIncrementPK() string {
	return "id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY"
}

func (mysqlDialect) CreateTableSuffix() string {
	return " DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci"
}

func (mysqlDialect) ListTables() string {
	return `SELECT TABLE_NAME FROM information_schema.TABLES ` +
		`WHERE TABLE_SCHEMA = DATABASE() AND TABLE_TYPE = 'BASE TABLE' ORDER BY TABLE_NAME`
}

func (mysqlDialect) TableExists(table string) (string, []any) {
	return `SELECT COUNT(*) FROM information_schema.TABLES ` +
		`WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ?`, []any{table}
}

func (mysqlDialect) TableColumns(table string) (string, []any) {
	query := `SELECT c.COLUMN_NAME, CASE WHEN k.COLUMN_NAME IS NULL THEN 0 ELSE 1 END
		FROM information_schema.COLUMNS c
		LEFT JOIN information_schema.KEY_COLUMN_USAGE k
		  ON k.TABLE_SCHEMA = c.TABLE_SCHEMA
		 AND k.TABLE_NAME = c.TABLE_NAME
		 AND k.COLUMN_NAME = c.COLUMN_NAME
		 AND k.CONSTRAINT_NAME = 'PRIMARY'
		WHERE c.TABLE_SCHEMA = DATABASE() AND c.TABLE_NAME = ?
		ORDER BY c.ORDINAL_POSITION`
	return query, []any{table}
}

// MySQL 不支持 CREATE INDEX IF NOT EXISTS（MariaDB 才支持），
// 所以索引是否存在要查 information_schema.STATISTICS。
func (mysqlDialect) IndexIfNotExists() bool { return false }

func (mysqlDialect) IndexExists(table, name string) (string, []any) {
	return `SELECT COUNT(*) FROM information_schema.STATISTICS ` +
			`WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND INDEX_NAME = ?`,
		[]any{table, name}
}

func (mysqlDialect) Setup() []string { return nil }

// ---------- PostgreSQL ----------

// Postgres 方言：双引号标识符、$n 占位符、BIGSERIAL 自增主键。
var Postgres Dialect = postgresDialect{}

type postgresDialect struct{}

func (postgresDialect) Name() string { return "postgres" }

func (postgresDialect) QuoteIdent(name string) string { return quoteDouble(name) }

func (postgresDialect) BindVar(i int) string { return "$" + strconv.Itoa(i+1) }

func (postgresDialect) AutoIncrementPK() string { return "id BIGSERIAL PRIMARY KEY" }

func (postgresDialect) CreateTableSuffix() string { return "" }

func (postgresDialect) ListTables() string {
	return `SELECT table_name FROM information_schema.tables ` +
		`WHERE table_schema = current_schema() AND table_type = 'BASE TABLE' ORDER BY table_name`
}

func (postgresDialect) TableExists(table string) (string, []any) {
	return `SELECT COUNT(*) FROM information_schema.tables ` +
		`WHERE table_schema = current_schema() AND table_name = $1`, []any{table}
}

func (postgresDialect) TableColumns(table string) (string, []any) {
	query := `SELECT c.column_name,
		CASE WHEN EXISTS (
			SELECT 1 FROM information_schema.table_constraints tc
			JOIN information_schema.key_column_usage kcu
			  ON kcu.constraint_name = tc.constraint_name
			 AND kcu.constraint_schema = tc.constraint_schema
			WHERE tc.constraint_type = 'PRIMARY KEY'
			  AND tc.table_schema = c.table_schema
			  AND tc.table_name = c.table_name
			  AND kcu.column_name = c.column_name
		) THEN 1 ELSE 0 END
		FROM information_schema.columns c
		WHERE c.table_schema = current_schema() AND c.table_name = $1
		ORDER BY c.ordinal_position`
	return query, []any{table}
}

func (postgresDialect) IndexIfNotExists() bool { return true }

func (postgresDialect) IndexExists(table, name string) (string, []any) {
	return `SELECT COUNT(*) FROM pg_indexes ` +
			`WHERE schemaname = current_schema() AND table_name = $1 AND indexname = $2`,
		[]any{table, name}
}

func (postgresDialect) Setup() []string { return nil }

// ---------- 通用工具 ----------

// quoteDouble 用双引号包裹标识符（SQLite / PostgreSQL），内部的 " 转义为 ""。
func quoteDouble(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// quoteBacktick 用反引号包裹标识符（MySQL），内部的反引号翻倍转义。
func quoteBacktick(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}

// quoteColumnList 用方言把列名列表转成逗号分隔的标识符串。
func quoteColumnList(d Dialect, cols []string) string {
	quoted := make([]string, len(cols))
	for i, c := range cols {
		quoted[i] = d.QuoteIdent(c)
	}
	return strings.Join(quoted, ", ")
}
