package file2db

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 选项函数最容易出的错是「定义了但忘了写进配置字段」，
// 所以这里逐个应用并断言落到哪个字段上。
func TestReadOptionsWiring(t *testing.T) {
	cases := []struct {
		name  string
		apply Option
		check func(readConfig) bool
	}{
		{"WithHeaderRow", WithHeaderRow(3), func(c readConfig) bool { return c.HeaderRow == 3 }},
		{"WithHeaderRow-忽略非正数", WithHeaderRow(0), func(c readConfig) bool { return c.HeaderRow == 1 }},
		{"WithSkipRows", WithSkipRows(2), func(c readConfig) bool { return c.SkipRows == 2 }},
		{"WithNoHeader", WithNoHeader(), func(c readConfig) bool { return c.NoHeader }},
		{"WithRawHeader", WithRawHeader(), func(c readConfig) bool { return c.KeepRawHeader }},
		{"WithSheet", WithSheet("s"), func(c readConfig) bool { return len(c.Sheets) == 1 && c.Sheets[0] == "s" }},
		{"WithSheets", WithSheets([]string{"a", "b"}), func(c readConfig) bool { return len(c.Sheets) == 2 }},
		{"WithSkipSheets", WithSkipSheets([]string{"x"}), func(c readConfig) bool { return len(c.SkipSheets) == 1 }},
		{"WithOnlyActiveSheet", WithOnlyActiveSheet(true), func(c readConfig) bool { return c.OnlyActiveSheet }},
		{"WithRawCellValue", WithRawCellValue(true), func(c readConfig) bool { return c.RawCellValue }},
		{"WithDelimiter", WithDelimiter('|'), func(c readConfig) bool { return c.Delimiter == '|' }},
		{"WithDelimiter-忽略零值", WithDelimiter(0), func(c readConfig) bool { return c.Delimiter == ',' }},
		{"WithEncoding", WithEncoding(EncodingGBK), func(c readConfig) bool { return c.Encoding == EncodingGBK }},
		{"WithLazyQuotes", WithLazyQuotes(false), func(c readConfig) bool { return !c.LazyQuotes }},
		{"WithComment", WithComment('#'), func(c readConfig) bool { return c.Comment == '#' }},
		{"WithTrimLeadingSpace", WithTrimLeadingSpace(true), func(c readConfig) bool { return c.TrimLeadingSpace }},
		{"WithFieldsPerRecord", WithFieldsPerRecord(9), func(c readConfig) bool { return c.FieldsPerRecord == 9 }},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := defaultReadConfig()
			c.apply(&cfg)
			if !c.check(cfg) {
				t.Errorf("%s 没有作用到 readConfig 上: %+v", c.name, cfg)
			}
		})
	}
}

func TestImportOptionsWiring(t *testing.T) {
	lg := log.New(os.Stderr, "", 0)

	cfg := defaultOptions()
	WithTable("t")(&cfg)
	WithDefaultTable("d")(&cfg)
	WithBatchSize(7)(&cfg)
	WithCreateTable(false)(&cfg)
	WithReplace()(&cfg)
	WithDefaultColumnType("INTEGER")(&cfg)
	WithColumnTypes(map[string]string{"a": "REAL"})(&cfg)
	WithKeepBlankRows()(&cfg)
	WithLogger(lg)(&cfg)
	WithProgress(3)(&cfg)
	WithImportDialect(MySQL)(&cfg)
	WithIndex("a", "b")(&cfg)
	WithUniqueIndex("c")(&cfg)
	WithIndexSpec(IndexSpec{Name: "i", Columns: []string{"d"}})(&cfg)

	switch {
	case cfg.Table != "t":
		t.Error("WithTable 未生效")
	case cfg.defaultTable != "d":
		t.Error("WithDefaultTable 未生效")
	case cfg.BatchSize != 7:
		t.Error("WithBatchSize 未生效")
	case cfg.CreateTable:
		t.Error("WithCreateTable(false) 未生效")
	case !cfg.Replace:
		t.Error("WithReplace 未生效")
	case cfg.DefaultType != "INTEGER":
		t.Error("WithDefaultColumnType 未生效")
	case cfg.ColumnTypes["a"] != "REAL":
		t.Error("WithColumnTypes 未生效")
	case !cfg.KeepBlankRows:
		t.Error("WithKeepBlankRows 未生效")
	case cfg.Logger != lg:
		t.Error("WithLogger 未生效")
	case cfg.ProgressEvery != 3:
		t.Error("WithProgress 未生效")
	case cfg.Dialect != MySQL:
		t.Error("WithImportDialect 未生效")
	case len(cfg.Indexes) != 3:
		t.Errorf("索引数 = %d, want 3（WithIndex/WithUniqueIndex/WithIndexSpec）", len(cfg.Indexes))
	case !cfg.Indexes[1].Unique:
		t.Error("WithUniqueIndex 未标记 Unique")
	case cfg.Indexes[2].Name != "i":
		t.Error("WithIndexSpec 未保留索引名")
	}
}

func TestExportOptionsWiring(t *testing.T) {
	lg := log.New(os.Stderr, "", 0)

	cfg := defaultExportOptions()
	WithExportFormat(FormatXLSX)(&cfg)
	WithExportIndex(false)(&cfg)
	WithExportTables("a", "b")(&cfg)
	WithExportSingleFile("all.xlsx")(&cfg)
	WithExportDialect(Postgres)(&cfg)
	WithExportDelimiter('\t')(&cfg)
	WithExportEncoding(EncodingGBK)(&cfg)
	WithExportBOM()(&cfg)
	WithExportLogger(lg)(&cfg)
	WithExportProgress(100)(&cfg)

	switch {
	case cfg.Format != FormatXLSX:
		t.Error("WithExportFormat 未生效")
	case cfg.Index:
		t.Error("WithExportIndex(false) 未生效")
	case len(cfg.Tables) != 2:
		t.Error("WithExportTables 未生效")
	case cfg.SingleFile != "all.xlsx":
		t.Error("WithExportSingleFile 未生效")
	case cfg.Dialect != Postgres:
		t.Error("WithExportDialect 未生效")
	case cfg.Delimiter != '\t':
		t.Error("WithExportDelimiter 未生效")
	case cfg.Encoding != EncodingGBK:
		t.Error("WithExportEncoding 未生效")
	case !cfg.BOM:
		t.Error("WithExportBOM 未生效")
	case cfg.Logger != lg:
		t.Error("WithExportLogger 未生效")
	case cfg.ProgressEvery != 100:
		t.Error("WithExportProgress 未生效")
	}
}

// MySQL / Postgres 的元数据 SQL 没法在本地真跑，但语句本身写错就是生产事故，
// 所以直接断言关键片段与占位符。
func TestMetadataSQLForMySQLAndPostgres(t *testing.T) {
	for _, d := range []Dialect{MySQL, Postgres} {
		t.Run(d.Name(), func(t *testing.T) {
			if d.ListTables() == "" {
				t.Error("ListTables 为空")
			}
			q, args := d.TableExists("t")
			if q == "" || len(args) != 1 {
				t.Errorf("TableExists 返回值不对: %q, %v", q, args)
			}
			q, args = d.TableColumns("t")
			// MySQL 用大写 information_schema.COLUMNS，Postgres 用小写，统一按不敏感比较
			if !strings.Contains(strings.ToLower(q), "information_schema.columns") || len(args) != 1 {
				t.Errorf("TableColumns 语句不对: %q", q)
			}
			q, args = d.IndexExists("t", "i")
			if q == "" || len(args) != 2 {
				t.Errorf("IndexExists 返回值不对: %q, %v", q, args)
			}
			// Setup：SQLite 有 PRAGMA，另外两种不需要
			if got := d.Setup(); len(got) != 0 {
				t.Errorf("%s 不应有初始化语句，得到 %v", d.Name(), got)
			}
		})
	}

	if MySQL.IndexIfNotExists() {
		t.Error("MySQL 不支持 CREATE INDEX IF NOT EXISTS")
	}
	if !SQLite.IndexIfNotExists() || !Postgres.IndexIfNotExists() {
		t.Error("SQLite / Postgres 支持 CREATE INDEX IF NOT EXISTS")
	}
	if len(SQLite.Setup()) == 0 {
		t.Error("SQLite 应带 PRAGMA 初始化语句")
	}
}

func TestSanitizeHeadersWrapper(t *testing.T) {
	got := SanitizeHeaders([]string{"NAME", "a b"})
	assertStrings(t, "SanitizeHeaders", got, []string{"name", "a_b"})
}

// WithSheet（单数）只导入指定的那一张表。
func TestImportExcelWithSheetSingle(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	path := writeXLSX(t, "single.xlsx", []sheetSpec{
		{name: "a", rows: [][]string{{"x"}, {"1"}}},
		{name: "b", rows: [][]string{{"y"}, {"2"}}},
	})

	res, err := Import(ctx, db, []string{path}, []Option{WithSheet("b")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertStrings(t, "表清单", res[0].Tables, []string{"b"})
}

// WithCreateTable(false)：目标表已存在时只写数据，不执行 DDL。
func TestImportWithoutCreateTable(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	if _, err := db.Exec(`CREATE TABLE pre (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT)`); err != nil {
		t.Fatal(err)
	}
	path := writeFile(t, "pre.csv", []byte("name\n张三\n"))
	if _, err := Import(ctx, db, []string{path}, nil,
		[]ImportOption{WithTable("pre"), WithCreateTable(false)}); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, db, "pre"); n != 1 {
		t.Errorf("行数 = %d, want 1", n)
	}
}

// WithDefaultTable 在数据源自己没有名字时兜底（内存切片就是这种情况）。
func TestImportWithDefaultTable(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	src := NewSlice([][]string{{"a"}, {"1"}})
	res, err := src.Import(ctx, db, WithDefaultTable("fallback"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Table != "fallback" {
		t.Errorf("表名 = %q, want fallback", res.Table)
	}
}

// 强制指定编码时不做探测，按给定编码解码。
func TestImportCSVForcedEncoding(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	path := writeFile(t, "forced.csv", mustGBK(t, "name\n张三\n"))
	if _, err := Import(ctx, db, []string{path}, []Option{WithEncoding(EncodingGBK)}, nil); err != nil {
		t.Fatal(err)
	}
	assertStrings(t, "name 列", queryStrings(t, db, `SELECT name FROM forced`), []string{"张三"})
}

// 注释行、前导空白由 CSV 选项控制。
func TestImportCSVCommentAndTrimSpace(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	path := writeFile(t, "comment.csv", []byte("# 这是注释\nname,age\n  张三 , 30\n"))
	_, err := Import(ctx, db, []string{path},
		[]Option{WithComment('#'), WithTrimLeadingSpace(true)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// 注释行不算数据
	if n := countRows(t, db, "comment"); n != 1 {
		t.Fatalf("行数 = %d, want 1（注释行应被忽略）", n)
	}
	got := queryStrings(t, db, `SELECT '[' || name || ']' FROM comment`)
	assertStrings(t, "前导空白已去掉", got, []string{"[张三 ]"}) // 只去掉前导，尾随空白保留
}

// WithExportTables 只导出指定的表。
func TestExportSelectedTables(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	for _, name := range []string{"t1", "t2"} {
		path := writeFile(t, name+".csv", []byte("a\n1\n"))
		if _, err := Import(ctx, db, []string{path}, nil, []ImportOption{WithTable(name)}); err != nil {
			t.Fatal(err)
		}
	}

	dir := filepath.Join(t.TempDir(), "out")
	results, err := ExportTables(ctx, db, dir, WithExportTables("t2"), WithExportIndex(false))
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Table != "t2" {
		t.Errorf("只应导出 t2，得到 %+v", results)
	}
	if _, err := os.Stat(filepath.Join(dir, "t1.csv")); !os.IsNotExist(err) {
		t.Error("t1 不应该被导出")
	}
}

func TestExportLoggerAndProgress(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	path := writeFile(t, "log.csv", []byte("a\n1\n2\n3\n"))
	if _, err := Import(ctx, db, []string{path}, nil, []ImportOption{WithTable("log")}); err != nil {
		t.Fatal(err)
	}

	var buf strings.Builder
	out := filepath.Join(t.TempDir(), "log.csv")
	if _, err := ExportTable(ctx, db, "log", out,
		WithExportIndex(false),
		WithExportLogger(newTestLogger(&buf)),
		WithExportProgress(2),
		WithExportDialect(SQLite)); err != nil {
		t.Fatal(err)
	}

	logText := buf.String()
	if !strings.Contains(logText, "已导出 2 行") {
		t.Errorf("导出进度日志不对:\n%s", logText)
	}
	// 不满一个进度间隔的行不打日志
	if strings.Contains(logText, "已导出 3 行") {
		t.Errorf("3 行不该打进度:\n%s", logText)
	}
}
