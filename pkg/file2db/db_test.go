package file2db

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenMemory(t *testing.T) {
	db, err := OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// 内存库必须强制单连接：多连接时每个连接是各自独立的库
	if got := db.Stats().MaxOpenConnections; got != 1 {
		t.Errorf("MaxOpenConnections = %d, want 1", got)
	}
	if db.Dialect().Name() != "sqlite" {
		t.Errorf("方言 = %q, want sqlite", db.Dialect().Name())
	}
	if _, err := db.Exec(`CREATE TABLE t (a TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO t VALUES ('x')`); err != nil {
		t.Fatal(err)
	}
	var got string
	if err := db.QueryRow(`SELECT a FROM t`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != "x" {
		t.Errorf("查到的值 = %q, want x", got)
	}
}

// 文件库默认单连接，并带上导入用的 PRAGMA。
func TestOpenFileDBPragmas(t *testing.T) {
	db := newTestDB(t)

	if got := db.Stats().MaxOpenConnections; got != 1 {
		t.Errorf("默认连接数 = %d, want 1", got)
	}

	var mode string
	if err := db.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(mode, "wal") {
		t.Errorf("journal_mode = %q, want wal", mode)
	}

	var busy int
	if err := db.QueryRow(`PRAGMA busy_timeout`).Scan(&busy); err != nil {
		t.Fatal(err)
	}
	if busy != 10000 {
		t.Errorf("busy_timeout = %d, want 10000", busy)
	}
}

func TestOpenWithExtraSetup(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "setup.db")
	db, err := Open("sqlite", dsn,
		WithMaxOpenConns(2),
		WithSetup("PRAGMA cache_size = -2000"),
		WithSQLitePragmas("PRAGMA temp_store = FILE"),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if got := db.Stats().MaxOpenConnections; got != 2 {
		t.Errorf("MaxOpenConnections = %d, want 2", got)
	}
	var size int
	if err := db.QueryRow(`PRAGMA cache_size`).Scan(&size); err != nil {
		t.Fatal(err)
	}
	if size != -2000 {
		t.Errorf("cache_size = %d, want -2000（用户追加的语句没生效）", size)
	}
}

// 初始化语句写错时必须打开失败，而不是带着坏配置继续跑。
func TestOpenSetupError(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "bad.db")
	if _, err := Open("sqlite", dsn, WithSetup("THIS IS NOT SQL")); err == nil {
		t.Fatal("非法初始化语句应当报错")
	}
}

func TestOpenBadDSN(t *testing.T) {
	bad := filepath.Join(t.TempDir(), "no-such-dir", "a.db")
	if _, err := Open("sqlite", bad); err == nil {
		t.Fatal("路径不存在时应当报错")
	}
}

func TestMetadataHelpers(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	if _, err := db.Exec(`CREATE TABLE "用户 数据" (id INTEGER PRIMARY KEY AUTOINCREMENT, "姓名" TEXT, age TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO "用户 数据" ("姓名", age) VALUES ('张三', '30'), ('李四', '25')`); err != nil {
		t.Fatal(err)
	}

	t.Run("Tables", func(t *testing.T) {
		tables, err := Tables(ctx, db)
		if err != nil {
			t.Fatal(err)
		}
		if len(tables) != 1 || tables[0] != "用户 数据" {
			t.Errorf("表清单 = %v, want [用户 数据]", tables)
		}
	})

	t.Run("TableExists", func(t *testing.T) {
		ok, err := TableExists(ctx, db, "用户 数据")
		if err != nil || !ok {
			t.Errorf("表应当存在: ok=%v err=%v", ok, err)
		}
		ok, err = TableExists(ctx, db, "nope")
		if err != nil || ok {
			t.Errorf("表不应存在: ok=%v err=%v", ok, err)
		}
	})

	t.Run("CountRows 表名带空格与中文", func(t *testing.T) {
		if n := countRows(t, db, "用户 数据"); n != 2 {
			t.Errorf("行数 = %d, want 2", n)
		}
	})

	t.Run("Columns", func(t *testing.T) {
		cols, err := Columns(ctx, db, "用户 数据")
		if err != nil {
			t.Fatal(err)
		}
		assertStrings(t, "列名", cols, []string{"id", "姓名", "age"})
	})

	t.Run("DropTable", func(t *testing.T) {
		if err := DropTable(ctx, db, "用户 数据"); err != nil {
			t.Fatal(err)
		}
		ok, err := TableExists(ctx, db, "用户 数据")
		if err != nil || ok {
			t.Errorf("表应已被删除: ok=%v err=%v", ok, err)
		}
		// 删不存在的表不报错（DROP TABLE IF EXISTS）
		if err := DropTable(ctx, db, "nope"); err != nil {
			t.Errorf("重复删除不应报错: %v", err)
		}
	})
}

// CountRows 的表名会被转义，所以表名里的引号不能造成注入。
func TestCountRowsEscapesIdentifier(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	weird := `a"b`
	if _, err := db.Exec(`CREATE TABLE "a""b" (x TEXT)`); err != nil {
		t.Fatalf("建表失败: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO "a""b" VALUES ('1')`); err != nil {
		t.Fatal(err)
	}
	n, err := CountRows(ctx, db, weird)
	if err != nil {
		t.Fatalf("带引号的表名应当能安全转义: %v", err)
	}
	if n != 1 {
		t.Errorf("行数 = %d, want 1", n)
	}
}

// WithDriveName / WithDialect 让 Open 的两个位置参数可以留空，也允许显式绑方言。
func TestOpenWithDialectAndDriveName(t *testing.T) {
	db, err := Open("", "",
		WithDriveName("sqlite"),
		WithDSN(filepath.Join(t.TempDir(), "opt.db")),
		WithDialect(Postgres),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// 显式指定的方言优先于按驱动名推断
	if db.Dialect().Name() != "postgres" {
		t.Errorf("方言 = %q, want postgres", db.Dialect().Name())
	}
}

// 方言自带的元数据 SQL 要能真的在库上跑通（SQLite 分支）。
func TestSQLiteMetadataSQL(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	if _, err := db.Exec(`CREATE TABLE m (a TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE INDEX idx_m_a ON m (a)`); err != nil {
		t.Fatal(err)
	}

	if q := SQLite.ListTables(); !strings.Contains(q, "sqlite_master") {
		t.Errorf("ListTables 语句不对: %s", q)
	}

	q, args := SQLite.TableColumns("m")
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		t.Fatalf("TableColumns 语句跑不通: %v", err)
	}
	if !rows.Next() {
		rows.Close()
		t.Fatal("TableColumns 没有返回列信息")
	}
	var name string
	var pk int
	err = rows.Scan(&name, &pk)
	rows.Close() // 单连接库：必须先释放，后面的查询才拿得到连接
	if err != nil {
		t.Fatal(err)
	}
	if name != "a" || pk != 0 {
		t.Errorf("TableColumns 第一列 = (%s, %d), want (a, 0)", name, pk)
	}

	// IndexExists 的 SQLite 分支（正常导入流程用不到，但方言实现必须正确）
	q, args = SQLite.IndexExists("m", "idx_m_a")
	var n int
	if err := db.QueryRowContext(ctx, q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("索引存在性查询返回 %d, want 1", n)
	}

	// 建索引前的查重逻辑（MySQL 走这条路径，这里用 SQLite 方言跑一遍函数体）
	ok, err := indexExists(ctx, db, SQLite, "m", "idx_m_a")
	if err != nil || !ok {
		t.Errorf("indexExists = %v, %v, want true, nil", ok, err)
	}
	ok, err = indexExists(ctx, db, SQLite, "m", "nope")
	if err != nil || ok {
		t.Errorf("不存在的索引 indexExists = %v, %v, want false, nil", ok, err)
	}
}
