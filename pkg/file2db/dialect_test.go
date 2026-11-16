package file2db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"strings"
	"testing"

	"github.com/jmoiron/sqlx"
)

func TestDialectForDriver(t *testing.T) {
	cases := []struct {
		driver string
		want   string
	}{
		{"mysql", "mysql"},
		{"MySQL", "mysql"},
		{"mariadb", "mysql"},
		{"postgres", "postgres"},
		{"postgresql", "postgres"},
		{"pgx", "postgres"},
		{"pgx/v5", "postgres"},
		{"lib/pq", "postgres"},
		{"pq", "postgres"},
		{"sqlite", "sqlite"},
		{"sqlite3", "sqlite"},
		{"unknown-driver", "sqlite"}, // 未知驱动兜底 SQLite
		{"", "sqlite"},
		{"  mysql  ", "mysql"},
	}
	for _, c := range cases {
		if got := DialectForDriver(c.driver).Name(); got != c.want {
			t.Errorf("DialectForDriver(%q).Name() = %q, want %q", c.driver, got, c.want)
		}
	}
}

func TestQuoteIdent(t *testing.T) {
	if got := SQLite.QuoteIdent(`a"b`); got != `"a""b"` {
		t.Errorf("SQLite 引号转义 = %s, want \"a\"\"b\"", got)
	}
	if got := Postgres.QuoteIdent(`a"b`); got != `"a""b"` {
		t.Errorf("Postgres 引号转义 = %s", got)
	}
	if got := MySQL.QuoteIdent("a`b"); got != "`a``b`" {
		t.Errorf("MySQL 反引号转义 = %s", got)
	}
	// 中文表名也能正确包裹
	if got := SQLite.QuoteIdent("邻区"); got != `"邻区"` {
		t.Errorf("中文标识符 = %s", got)
	}
	// 关键字不特殊处理，交给引号
	if got := SQLite.QuoteIdent("select"); got != `"select"` {
		t.Errorf("关键字标识符 = %s", got)
	}
}

func TestBindVar(t *testing.T) {
	if got := SQLite.BindVar(0); got != "?" {
		t.Errorf("SQLite.BindVar(0) = %q", got)
	}
	if got := MySQL.BindVar(9); got != "?" {
		t.Errorf("MySQL.BindVar(9) = %q", got)
	}
	if got := Postgres.BindVar(0); got != "$1" {
		t.Errorf("Postgres.BindVar(0) = %q", got)
	}
	if got := Postgres.BindVar(2); got != "$3" {
		t.Errorf("Postgres.BindVar(2) = %q", got)
	}
}

func TestBuildCreateTableSQL(t *testing.T) {
	headers := []string{"name", "age"}
	types := map[string]string{"age": "INTEGER"}

	sqlite := buildCreateTableSQL(SQLite, "users", headers, types, "TEXT")
	if !strings.Contains(sqlite, `CREATE TABLE IF NOT EXISTS "users"`) {
		t.Errorf("SQLite 建表语句缺表名: %s", sqlite)
	}
	if !strings.Contains(sqlite, `id INTEGER PRIMARY KEY AUTOINCREMENT`) {
		t.Errorf("SQLite 建表语句缺自增主键: %s", sqlite)
	}
	if !strings.Contains(sqlite, `"age" INTEGER`) || !strings.Contains(sqlite, `"name" TEXT`) {
		t.Errorf("SQLite 列类型不对: %s", sqlite)
	}

	pg := buildCreateTableSQL(Postgres, "users", headers, nil, "TEXT")
	if !strings.Contains(pg, "BIGSERIAL PRIMARY KEY") {
		t.Errorf("Postgres 建表语句缺 BIGSERIAL: %s", pg)
	}

	my := buildCreateTableSQL(MySQL, "users", headers, nil, "TEXT")
	if !strings.Contains(my, "`users`") || !strings.Contains(my, "AUTO_INCREMENT") {
		t.Errorf("MySQL 建表语句不对: %s", my)
	}
	if !strings.Contains(my, "DEFAULT CHARSET=utf8mb4") {
		t.Errorf("MySQL 建表语句缺字符集: %s", my)
	}
	// MySQL 用反引号，不能出现双引号包裹的标识符
	if strings.Contains(my, `"users"`) {
		t.Errorf("MySQL 不应使用双引号标识符: %s", my)
	}
}

func TestBuildInsertSQL(t *testing.T) {
	headers := []string{"name", "age"}

	for _, d := range []Dialect{SQLite, MySQL} {
		got := buildInsertSQL(d, "t", headers)
		want := "INSERT INTO " + d.QuoteIdent("t") + " (" + quoteColumnList(d, headers) + ") VALUES (?, ?)"
		if got != want {
			t.Errorf("%s 插入语句 = %s, want %s", d.Name(), got, want)
		}
	}

	got := buildInsertSQL(Postgres, "t", headers)
	want := `INSERT INTO "t" ("name", "age") VALUES ($1, $2)`
	if got != want {
		t.Errorf("Postgres 插入语句 = %s, want %s", got, want)
	}
}

func TestBuildCreateIndexSQL(t *testing.T) {
	got := buildCreateIndexSQL(SQLite, "users", "idx_users_name", []string{"name", "age"}, false)
	want := `CREATE INDEX IF NOT EXISTS "idx_users_name" ON "users" ("name", "age")`
	if got != want {
		t.Errorf("SQLite 建索引 = %s, want %s", got, want)
	}

	got = buildCreateIndexSQL(Postgres, "users", "idx_u", []string{"name"}, true)
	want = `CREATE UNIQUE INDEX IF NOT EXISTS "idx_u" ON "users" ("name")`
	if got != want {
		t.Errorf("Postgres 唯一索引 = %s, want %s", got, want)
	}

	// MySQL 不支持 IF NOT EXISTS，语句里不能出现它
	got = buildCreateIndexSQL(MySQL, "users", "idx_u", []string{"name"}, false)
	if strings.Contains(got, "IF NOT EXISTS") {
		t.Errorf("MySQL 建索引不应带 IF NOT EXISTS: %s", got)
	}
	if !strings.Contains(got, "`idx_u`") || !strings.Contains(got, "`users`") {
		t.Errorf("MySQL 建索引标识符不对: %s", got)
	}
}

func TestQuoteColumnList(t *testing.T) {
	if got := quoteColumnList(SQLite, []string{"a", "b c"}); got != `"a", "b c"` {
		t.Errorf("quoteColumnList = %s", got)
	}
	if got := quoteColumnList(MySQL, []string{"a"}); got != "`a`" {
		t.Errorf("quoteColumnList(MySQL) = %s", got)
	}
}

// TestDialectOfInference 覆盖方言推断的四档优先级：
// 自带 Dialect() > DriverName() > 底层驱动类型 > SQLite 兜底。
func TestDialectOfInference(t *testing.T) {
	t.Run("本包-DB-自带方言", func(t *testing.T) {
		db, err := OpenMemory()
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		if got := dialectOf(db).Name(); got != "sqlite" {
			t.Errorf("OpenMemory 方言 = %q, want sqlite", got)
		}
	})

	t.Run("sqlx-按驱动名推断", func(t *testing.T) {
		raw, err := sql.Open("sqlite", ":memory:")
		if err != nil {
			t.Fatal(err)
		}
		defer raw.Close()

		// 驱动名说是 postgres，就应推成 Postgres（这里不真的连库）
		sdb := sqlx.NewDb(raw, "postgres")
		if got := dialectOf(sdb).Name(); got != "postgres" {
			t.Errorf("sqlx(postgres) 方言 = %q, want postgres", got)
		}
	})

	t.Run("裸-sql.DB-按驱动类型推断", func(t *testing.T) {
		raw, err := sql.Open("sqlite", ":memory:")
		if err != nil {
			t.Fatal(err)
		}
		defer raw.Close()
		if got := dialectOf(raw).Name(); got != "sqlite" {
			t.Errorf("裸 *sql.DB(sqlite) 方言 = %q, want sqlite", got)
		}
	})

	t.Run("认不出来兜底-SQLite", func(t *testing.T) {
		if got := dialectOf(fakeDatabase{}).Name(); got != "sqlite" {
			t.Errorf("未知数据库方言 = %q, want sqlite", got)
		}
	})
}

func TestPickDialectPrefersExplicit(t *testing.T) {
	db, err := OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if got := pickDialect(nil, db).Name(); got != "sqlite" {
		t.Errorf("未指定时应用数据源方言，得到 %q", got)
	}
	if got := pickDialect(MySQL, db).Name(); got != "mysql" {
		t.Errorf("显式方言应优先，得到 %q", got)
	}
}

func TestWrap(t *testing.T) {
	raw, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()

	// d 传 nil：按驱动名推断
	got := Wrap(raw, nil)
	if got == nil {
		t.Fatal("Wrap 返回 nil")
	}
	if got.Dialect().Name() != "sqlite" {
		t.Errorf("Wrap 推断方言 = %q, want sqlite", got.Dialect().Name())
	}
	// d 显式指定
	if got := Wrap(raw, Postgres); got.Dialect().Name() != "postgres" {
		t.Errorf("Wrap 显式方言 = %q, want postgres", got.Dialect().Name())
	}
	if Wrap(nil, nil) != nil {
		t.Error("Wrap(nil) 应返回 nil")
	}
}

// fakeDatabase 是完全认不出方言的 Database 实现（没有 Dialect()、也没有 DriverName()），
// 用来验证「推断不出来时兜底 SQLite」。所有方法都不会被执行。
type fakeDatabase struct{}

func (fakeDatabase) Exec(string, ...any) (sql.Result, error)                         { return nil, nil }
func (fakeDatabase) ExecContext(context.Context, string, ...any) (sql.Result, error) { return nil, nil }
func (fakeDatabase) Query(string, ...any) (*sql.Rows, error)                         { return nil, nil }
func (fakeDatabase) QueryContext(context.Context, string, ...any) (*sql.Rows, error) { return nil, nil }
func (fakeDatabase) QueryRow(string, ...any) *sql.Row                                { return nil }
func (fakeDatabase) QueryRowContext(context.Context, string, ...any) *sql.Row        { return nil }
func (fakeDatabase) Begin() (*sql.Tx, error)                                         { return nil, nil }
func (fakeDatabase) BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)        { return nil, nil }

// 下面三个假驱动用来验证「拿不到驱动名时按驱动类型的关键词猜方言」。
type mysqlDriver struct{}
type pgxDriver struct{}
type plainDriver struct{}

func (mysqlDriver) Open(string) (driver.Conn, error) { return nil, nil }
func (pgxDriver) Open(string) (driver.Conn, error)   { return nil, nil }
func (plainDriver) Open(string) (driver.Conn, error) { return nil, nil }

func TestDialectFromDriver(t *testing.T) {
	cases := []struct {
		name   string
		driver driver.Driver
		want   Dialect
	}{
		{"nil", nil, nil},
		{"mysql", mysqlDriver{}, MySQL},
		{"pgx", pgxDriver{}, Postgres},
		{"认不出来的驱动", plainDriver{}, nil},
	}
	for _, c := range cases {
		got := dialectFromDriver(c.driver)
		switch {
		case c.want == nil && got != nil:
			t.Errorf("%s: 期望推断不出来，得到 %s", c.name, got.Name())
		case c.want != nil && got != c.want:
			t.Errorf("%s: 推断方言 = %v, want %v", c.name, got, c.want)
		}
	}
}
