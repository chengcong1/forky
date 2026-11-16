package file2db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"reflect"
	"strings"

	_ "modernc.org/sqlite" // 默认驱动：SQLite（纯 Go，无需 CGO）
)

// DriverName 是默认使用的 SQLite 驱动名。
//
// 支持其它数据库时，请自行空导入对应驱动，例如：
//
//	import (
//		_ "github.com/go-sql-driver/mysql" // 驱动名 "mysql"
//		_ "github.com/lib/pq"              // 驱动名 "postgres"
//	)
const DriverName = "sqlite"

// Database 是各导入 / 导出函数接收的最小接口。
//
// 除了本包的 *DB，外部连接池也直接能用：*sql.DB、*sqlx.DB 都满足它
// （sqlx.DB 内嵌了 *sql.DB，方法会自动提升）。
//
// 方言按以下顺序解析：*DB 自带 > DriverName()（sqlx）> 底层驱动类型 > SQLite；
// 也可以用 WithImportDialect / WithExportDialect / Wrap 显式指定。
//
// 导入 / 导出都要求传 context：它决定超时与 Ctrl+C 能否中断一个长任务，
// 也决定了中途取消时事务会不会被回滚。
type Database interface {
	Exec(query string, args ...any) (sql.Result, error)
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	Begin() (*sql.Tx, error)
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

// DB 包装 *sql.DB 并把方言一起带在身上。
// 因为内嵌了 *sql.DB，所有 *sql.DB 的方法都能直接用。
type DB struct {
	*sql.DB
	dialect Dialect
}

// Dialect 返回该连接使用的方言。
func (d *DB) Dialect() Dialect { return d.dialect }

// Wrap 把已经建好的连接池包成 *DB 并绑定方言，适合复用外部创建的连接
// （sql.Open、sqlx.NewDb、连接池库等）。
//
// d 传 nil 时自动推断：
//
//	db, _ := sql.Open("mysql", dsn)          // 或 sqlx.NewDb(raw, "mysql")
//
// fdb := file2db.Wrap(db, file2db.MySQL)    // 推不出来时显式指定
func Wrap(db *sql.DB, d Dialect) *DB {
	if db == nil {
		return nil
	}
	if d == nil {
		d = dialectOf(db)
	}
	return &DB{DB: db, dialect: d}
}

// dialectOf 解析数据源方言，优先级从高到低：
//
//  1. 实现了 Dialect() Dialect 的包装（本包 *DB）；
//  2. 能拿到驱动名的（*sqlx.DB 的 DriverName()）；
//  3. 由底层驱动类型推断（裸 *sql.DB 的 Driver()）；
//  4. 兜底 SQLite。
func dialectOf(db Database) Dialect {
	if d, ok := db.(interface{ Dialect() Dialect }); ok {
		if got := d.Dialect(); got != nil {
			return got
		}
	}
	if n, ok := db.(interface{ DriverName() string }); ok {
		if name := n.DriverName(); name != "" {
			return DialectForDriver(name)
		}
	}
	if d, ok := db.(interface{ Driver() driver.Driver }); ok {
		if got := dialectFromDriver(d.Driver()); got != nil {
			return got
		}
	}
	return SQLite
}

// dialectFromDriver 由驱动实例推断方言：拿不到驱动名时，
// 用驱动类型的包路径 / 类型名做关键词匹配，认不出来返回 nil。
//
// 覆盖常见驱动：modernc.org/sqlite、go-sql-driver/mysql、lib/pq、pgx/stdlib。
func dialectFromDriver(d driver.Driver) Dialect {
	t := reflect.TypeOf(d)
	if t == nil {
		return nil
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	hint := strings.ToLower(t.PkgPath() + " " + t.String())
	switch {
	case hint == "":
		return nil
	case strings.Contains(hint, "mysql"), strings.Contains(hint, "mariadb"):
		return MySQL
	case strings.Contains(hint, "pgx"), strings.Contains(hint, "postgres"), strings.Contains(hint, "lib/pq"):
		return Postgres
	case strings.Contains(hint, "sqlite"):
		return SQLite
	}
	return nil
}

// pickDialect 优先用显式指定的方言，否则从数据源推断。
func pickDialect(explicit Dialect, db Database) Dialect {
	if explicit != nil {
		return explicit
	}
	return dialectOf(db)
}

// DBConfig 数据库连接配置。
type DBConfig struct {
	DriveName    string
	DSN          string
	Dialect      Dialect // 留空时按驱动名推断（mysql -> MySQL，postgres/pgx -> Postgres，其它 -> SQLite）
	MaxOpenConns int
	Pragmas      []string // 连接建立后额外执行的语句，追加在方言自带语句之后
}

// DBOption 用于配置数据库连接。
type DBOption func(*DBConfig)

// defaultDBConfig 返回连接配置的默认值：驱动 sqlite、数据源 data.db、单连接。
func defaultDBConfig() DBConfig {
	return DBConfig{
		DriveName:    DriverName,
		DSN:          "data.db",
		MaxOpenConns: 1,
	}
}

// WithDialect 显式指定方言（一般不需要：Open 会按驱动名推断）。
func WithDialect(d Dialect) DBOption {
	return func(c *DBConfig) { c.Dialect = d }
}

// WithDriveName 指定 SQL 驱动名（默认 sqlite）。
func WithDriveName(name string) DBOption {
	return func(c *DBConfig) {
		if name != "" {
			c.DriveName = name
		}
	}
}

// WithDSN 指定数据源名：SQLite 是文件路径或 ":memory:"，
// MySQL/Postgres 则是各驱动自己的 DSN。
func WithDSN(dsn string) DBOption {
	return func(c *DBConfig) {
		if dsn != "" {
			c.DSN = dsn
		}
	}
}

// WithMaxOpenConns 设置连接池上限。默认 1：
// 导入时事务独占唯一连接，可避免「连接池取不到连接」造成的死锁。
func WithMaxOpenConns(n int) DBOption {
	return func(c *DBConfig) {
		if n > 0 {
			c.MaxOpenConns = n
		}
	}
}

// WithSQLitePragmas 追加额外的 SQLite PRAGMA 语句（方言自带的 PRAGMA 在前）。
func WithSQLitePragmas(p ...string) DBOption {
	return func(c *DBConfig) { c.Pragmas = append(c.Pragmas, p...) }
}

// WithSetup 追加连接建立后要执行的语句，不限于 PRAGMA
// （例如 MySQL 的 "SET NAMES utf8mb4"）。
func WithSetup(stmts ...string) DBOption {
	return func(c *DBConfig) { c.Pragmas = append(c.Pragmas, stmts...) }
}

// Open 打开数据库并执行方言自带的初始化语句。
//
// driveName / dsn 留空时使用 WithDriveName / WithDSN 指定的值（默认 sqlite + data.db）。
// 方言按驱动名推断，也可用 WithDialect 指定。
// 返回的 *DB 由调用方负责 Close（内嵌 *sql.DB，可直接当 *sql.DB 用）。
//
//	db, err := file2db.Open("mysql", "user:pass@tcp(127.0.0.1:3306)/db?charset=utf8mb4")
//	db, err := file2db.Open("postgres", "postgres://user:pass@127.0.0.1/db?sslmode=disable")
func Open(driveName string, dsn string, opts ...DBOption) (*DB, error) {
	cfg := defaultDBConfig()
	for _, o := range opts {
		o(&cfg)
	}
	if driveName == "" {
		driveName = cfg.DriveName
	}
	if dsn == "" {
		dsn = cfg.DSN
	}
	if cfg.Dialect == nil {
		cfg.Dialect = DialectForDriver(driveName)
	}

	db, err := sql.Open(driveName, dsn)
	if err != nil {
		return nil, fmt.Errorf("file2db: 打开数据库失败 %s: %w", dsn, err)
	}
	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxOpenConns)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("file2db: 连接数据库失败 %s: %w", dsn, err)
	}

	// 方言自带的初始化语句（SQLite 是 PRAGMA）+ 用户追加的语句
	setup := append(append([]string(nil), cfg.Dialect.Setup()...), cfg.Pragmas...)
	for _, stmt := range setup {
		if _, err := db.Exec(stmt); err != nil {
			db.Close()
			return nil, fmt.Errorf("file2db: 执行 %q 失败: %w", stmt, err)
		}
	}
	return &DB{DB: db, dialect: cfg.Dialect}, nil
}

// OpenMemory 打开一个私有的 SQLite 内存库，常用于测试或临时分析。
func OpenMemory(opts ...DBOption) (*DB, error) {
	// 多连接时 ":memory:" 每个连接都是独立库，因此强制单连接
	opts = append([]DBOption{WithDSN(":memory:"), WithMaxOpenConns(1)}, opts...)
	return Open("", "", opts...)
}

// Tables 返回库中所有用户表名。
func Tables(ctx context.Context, db Database) ([]string, error) {
	rows, err := db.QueryContext(ctx, dialectOf(db).ListTables())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

// TableExists 判断表是否存在。
func TableExists(ctx context.Context, db Database, table string) (bool, error) {
	query, args := dialectOf(db).TableExists(table)
	var n int
	if err := db.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

// CountRows 统计表行数（表名会被安全转义）。
func CountRows(ctx context.Context, db Database, table string) (int64, error) {
	var n int64
	query := "SELECT COUNT(*) FROM " + dialectOf(db).QuoteIdent(table)
	err := db.QueryRowContext(ctx, query).Scan(&n)
	return n, err
}

// Columns 返回表的列名。
func Columns(ctx context.Context, db Database, table string) ([]string, error) {
	if _, err := TableExists(ctx, db, table); err != nil {
		return nil, err
	}
	query := "SELECT * FROM " + dialectOf(db).QuoteIdent(table) + " LIMIT 0"
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return rows.Columns()
}

// DropTable 删除表（WithReplace 内部使用）。
func DropTable(ctx context.Context, db Database, table string) error {
	_, err := db.ExecContext(ctx, "DROP TABLE IF EXISTS "+dialectOf(db).QuoteIdent(table))
	return err
}
