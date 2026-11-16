package file2db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
)

// 不加 WithReplace 是追加，加了才是全量覆盖。
func TestImportAppendAndReplace(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	path := writeFile(t, "a.csv", []byte("name\n张三\n李四\n"))

	mustImport(t, ctx, db, path, nil)
	if n := countRows(t, db, "a"); n != 2 {
		t.Fatalf("首次导入后 %d 行, want 2", n)
	}

	mustImport(t, ctx, db, path, nil)
	if n := countRows(t, db, "a"); n != 4 {
		t.Errorf("追加导入后 %d 行, want 4", n)
	}

	mustImport(t, ctx, db, path, []ImportOption{WithReplace()})
	if n := countRows(t, db, "a"); n != 2 {
		t.Errorf("WithReplace 后 %d 行, want 2", n)
	}
}

// 每 BatchSize 行提交一次并重新预编译，写入的内容不能错位或漏行。
func TestImportBatchSizeCommitAndReprepare(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	var sb strings.Builder
	sb.WriteString("n\n")
	for i := 1; i <= 7; i++ {
		fmt.Fprintf(&sb, "%d\n", i)
	}
	path := writeFile(t, "batch.csv", []byte(sb.String()))

	res, err := Import(ctx, db, []string{path}, nil, []ImportOption{WithBatchSize(2)})
	if err != nil {
		t.Fatal(err)
	}
	if res[0].Rows != 7 {
		t.Fatalf("Rows = %d, want 7", res[0].Rows)
	}
	// 顺序必须与文件一致（按自增 id 读回来）
	got := queryStrings(t, db, `SELECT n FROM batch ORDER BY id`)
	assertStrings(t, "分批写入结果", got, []string{"1", "2", "3", "4", "5", "6", "7"})
}

func TestImportColumnTypes(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	path := writeFile(t, "ct.csv", []byte("name,age\n张三,30\n"))
	_, err := Import(ctx, db, []string{path}, nil, []ImportOption{
		WithColumnTypes(map[string]string{"age": "INTEGER"}),
		WithDefaultColumnType("TEXT"),
	})
	if err != nil {
		t.Fatal(err)
	}

	ddl := tableSQL(t, db, "ct")
	if !strings.Contains(ddl, `"age" INTEGER`) {
		t.Errorf("age 列类型不是 INTEGER: %s", ddl)
	}
	if !strings.Contains(ddl, `"name" TEXT`) {
		t.Errorf("name 列应为默认 TEXT: %s", ddl)
	}
}

func TestImportIndexes(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	path := writeFile(t, "idx.csv", []byte("name,age\n张三,30\n李四,25\n"))

	t.Run("普通索引在写完后创建", func(t *testing.T) {
		res, err := Import(ctx, db, []string{path}, nil,
			[]ImportOption{WithTable("idx1"), WithIndex("name")})
		if err != nil {
			t.Fatal(err)
		}
		if len(res[0].Indexes) != 1 {
			t.Fatalf("应建 1 个索引，得到 %v", res[0].Indexes)
		}
		if res[0].Indexes[0] != "idx_idx1_name" {
			t.Errorf("索引名 = %q, want idx_idx1_name", res[0].Indexes[0])
		}
		if n := queryStrings(t, db,
			`SELECT name FROM sqlite_master WHERE type='index' AND tbl_name='idx1' AND name=?`,
			res[0].Indexes[0]); len(n) != 1 {
			t.Errorf("索引没落到库里: %v", n)
		}
	})

	t.Run("自定义索引名与唯一索引", func(t *testing.T) {
		res, err := Import(ctx, db, []string{path}, nil,
			[]ImportOption{WithTable("idx2"), WithIndexSpec(IndexSpec{Name: "uniq_age", Columns: []string{"age"}, Unique: true})})
		if err != nil {
			t.Fatal(err)
		}
		assertStrings(t, "索引名", res[0].Indexes, []string{"uniq_age"})
	})

	t.Run("列名对不上就跳过该索引", func(t *testing.T) {
		res, err := Import(ctx, db, []string{path}, nil,
			[]ImportOption{WithTable("idx3"), WithIndex("nope")})
		if err != nil {
			t.Fatal(err)
		}
		if len(res[0].Indexes) != 0 {
			t.Errorf("对不上的列不应建索引，得到 %v", res[0].Indexes)
		}
	})

	t.Run("唯一索引遇到重复值报错", func(t *testing.T) {
		dup := writeFile(t, "dup.csv", []byte("name\n张三\n张三\n"))
		// 索引是数据全部落库之后才建的，所以这里数据会写进去、索引创建失败
		_, err := Import(ctx, db, []string{dup}, nil,
			[]ImportOption{WithTable("idx4"), WithUniqueIndex("name")})
		if err == nil {
			t.Fatal("重复值 + 唯一索引应当报错")
		}
		if !strings.Contains(err.Error(), "索引") {
			t.Errorf("错误信息应指向建索引失败: %v", err)
		}
		if n := countRows(t, db, "idx4"); n != 2 {
			t.Errorf("数据应已提交（%d 行），索引失败不影响已写入的数据", n)
		}
	})
}

// 提前取消：立刻返回 context.Canceled，并且不留下半张表。
func TestImportContextAlreadyCancelled(t *testing.T) {
	db := newTestDB(t)
	path := writeFile(t, "never.csv", []byte("a\n1\n"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := Import(ctx, db, []string{path}, nil, []ImportOption{WithTable("never")})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("错误应为 context.Canceled，得到 %v", err)
	}
	exists, err := TableExists(context.Background(), db, "never")
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Error("取消后不应建表")
	}
}

// 中途取消：已提交的批次保留，且提交行数一定是 BatchSize 的整数倍
// （说明失败的那一批被完整回滚，没有半批数据）。
func TestImportContextCancelMidway(t *testing.T) {
	dir := t.TempDir()
	dsn := filepath.Join(dir, "big.db")
	const batch = 500
	const total = 200000

	db, err := Open("sqlite", dsn, WithMaxOpenConns(1))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var sb strings.Builder
	sb.WriteString("n\n")
	for i := 1; i <= total; i++ {
		fmt.Fprintf(&sb, "%d\n", i)
	}
	path := writeFile(t, "big.csv", []byte(sb.String()))

	// 另一个连接只读探测：等第一批提交后取消
	watch, err := Open("sqlite", dsn, WithMaxOpenConns(1))
	if err != nil {
		t.Fatal(err)
	}
	defer watch.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		for i := 0; i < 2000; i++ {
			if hasAnyRow(watch, "big") {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		cancel()
	}()

	_, err = Import(ctx, db, []string{path}, nil,
		[]ImportOption{WithTable("big"), WithBatchSize(batch)})
	if err == nil {
		t.Skip("导入在取消生效前就跑完了，跳过（机器太快）")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("错误应为 context.Canceled，得到 %v", err)
	}

	n := countRows(t, db, "big")
	if n == 0 {
		t.Fatal("取消前至少应提交一批数据")
	}
	if n%batch != 0 {
		t.Errorf("已提交 %d 行，不是 BatchSize(%d) 的整数倍，说明回滚不完整", n, batch)
	}
	if n >= total {
		t.Errorf("已提交 %d 行，应该小于总数 %d（取消要真的生效）", n, total)
	}
}

func TestImportBadArguments(t *testing.T) {
	ctx := context.Background()

	if _, err := Import(ctx, nil, []string{"a.csv"}, nil, nil); err == nil {
		t.Error("db 为 nil 应当报错")
	}
	db := newTestDB(t)
	if _, err := Import(ctx, db, nil, nil, nil); err == nil {
		t.Error("paths 为空应当报错")
	}
}

// Imports 一步完成「开库 + 导入」，失败时不把半成品连接交出去。
func TestImportsOneStep(t *testing.T) {
	ctx := context.Background()
	dsn := filepath.Join(t.TempDir(), "one.db")
	path := writeFile(t, "one.csv", []byte("name\n张三\n"))

	db, err := Imports(ctx, []string{path},
		[]DBOption{WithDSN(dsn)},
		[]Option{WithRawHeader()},
		[]ImportOption{WithReplace()},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if n := countRows(t, db, "one"); n != 1 {
		t.Errorf("表里 %d 行, want 1", n)
	}
	if db.Dialect().Name() != "sqlite" {
		t.Errorf("方言 = %q, want sqlite", db.Dialect().Name())
	}
}

// Imports 打开库之后如果导入失败，必须把库关掉再返回，不能把半成品连接交给调用方。
func TestImportsFailure(t *testing.T) {
	ctx := context.Background()

	t.Run("文件不存在", func(t *testing.T) {
		db, err := Imports(ctx, []string{filepath.Join(t.TempDir(), "nope.csv")},
			[]DBOption{WithDSN(filepath.Join(t.TempDir(), "f1.db"))}, nil, nil)
		if err == nil {
			db.Close()
			t.Fatal("文件不存在时应当报错")
		}
		if db != nil {
			t.Error("失败时不应返回连接")
		}
	})

	t.Run("路径为空", func(t *testing.T) {
		if _, err := Imports(ctx, nil, nil, nil, nil); err == nil {
			t.Fatal("paths 为空应当报错")
		}
	})
}

// 外部创建的连接池（*sql.DB / *sqlx.DB）必须能直接当目标库用。
func TestImportWithExternalPool(t *testing.T) {
	ctx := context.Background()
	path := writeFile(t, "ext.csv", []byte("name\n张三\n"))

	t.Run("sqlx", func(t *testing.T) {
		raw, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "ext1.db"))
		if err != nil {
			t.Fatal(err)
		}
		defer raw.Close()
		raw.SetMaxOpenConns(1)

		sdb := sqlx.NewDb(raw, "sqlite")
		if _, err := Import(ctx, sdb, []string{path}, nil, []ImportOption{WithTable("ext1")}); err != nil {
			t.Fatal(err)
		}
		if n := countRows(t, sdb, "ext1"); n != 1 {
			t.Errorf("表里 %d 行, want 1", n)
		}
	})

	t.Run("裸 sql.DB", func(t *testing.T) {
		raw, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "ext2.db"))
		if err != nil {
			t.Fatal(err)
		}
		defer raw.Close()
		raw.SetMaxOpenConns(1)

		if _, err := Import(ctx, raw, []string{path}, nil, []ImportOption{WithTable("ext2")}); err != nil {
			t.Fatal(err)
		}
		if n := countRows(t, raw, "ext2"); n != 1 {
			t.Errorf("表里 %d 行, want 1", n)
		}
	})
}

// ------------------------------ 小工具 ------------------------------

func mustImport(t *testing.T, ctx context.Context, db Database, path string, impOpts []ImportOption) {
	t.Helper()
	if _, err := Import(ctx, db, []string{path}, nil, impOpts); err != nil {
		t.Fatalf("导入 %s 失败: %v", path, err)
	}
}

// hasAnyRow 用最便宜的查询探测表里是否已有数据（给取消测试轮询用，忽略错误）。
func hasAnyRow(db Database, table string) bool {
	var one int
	err := db.QueryRow("SELECT 1 FROM " + SQLite.QuoteIdent(table) + " LIMIT 1").Scan(&one)
	return err == nil
}
