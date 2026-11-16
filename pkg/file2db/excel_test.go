package file2db

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImportExcelBasic(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	path := writeXLSX(t, "users.xlsx", []sheetSpec{
		{name: "users", rows: [][]string{
			{"Name", "Age"},
			{"张三", "30"},
			{"李四", "25"},
		}},
	})

	res, err := Import(ctx, db, []string{path}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || len(res[0].Tables) != 1 {
		t.Fatalf("结果结构不对: %+v", res)
	}
	if res[0].Tables[0] != "users" {
		t.Errorf("表名 = %q, want users（取 Sheet 名）", res[0].Tables[0])
	}
	if res[0].Rows != 2 {
		t.Errorf("Rows = %d, want 2", res[0].Rows)
	}
	assertStrings(t, "列名", res[0].Columns, []string{"name", "age"})
	assertStrings(t, "数据", queryStrings(t, db, `SELECT name FROM users`), []string{"张三", "李四"})
}

// Excel 稀疏存储：行尾的空单元格不落文件，行长度会小于表头列数，必须补齐。
func TestImportExcelSparseRows(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	path := writeXLSX(t, "sparse.xlsx", []sheetSpec{
		{name: "s", rows: [][]string{
			{"a", "b", "c"},
			{"1"},      // 只有第一列
			{"1", "2"}, // 前两列
			{"1", "2", "3"},
		}},
	})

	if _, err := Import(ctx, db, []string{path}, nil, nil); err != nil {
		t.Fatal(err)
	}
	got := queryStrings(t, db, `SELECT a || '|' || b || '|' || c FROM s ORDER BY id`)
	assertStrings(t, "稀疏行补齐", got, []string{"1||", "1|2|", "1|2|3"})
}

func TestImportExcelMultiSheet(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	path := writeXLSX(t, "multi.xlsx", []sheetSpec{
		{name: "users", rows: [][]string{{"name"}, {"张三"}}},
		{name: "orders", rows: [][]string{{"order"}, {"A1"}, {"A2"}}},
	})

	res, err := Import(ctx, db, []string{path}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertStrings(t, "表清单", res[0].Tables, []string{"users", "orders"})
	if res[0].Rows != 3 {
		t.Errorf("总行数 = %d, want 3", res[0].Rows)
	}
	if res[0].Table != "" {
		t.Errorf("多表时 Result.Table 应为空，得到 %q", res[0].Table)
	}
}

// 回归：WithSheets 与 WithSkipSheets 必须同时生效（以前给了 WithSheets 就会丢掉 SkipSheets）。
func TestImportExcelSheetFilter(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	path := writeXLSX(t, "filter.xlsx", []sheetSpec{
		{name: "keep", rows: [][]string{{"a"}, {"1"}}},
		{name: "drop", rows: [][]string{{"b"}, {"2"}}},
		{name: "other", rows: [][]string{{"c"}, {"3"}}},
	})

	t.Run("只点名", func(t *testing.T) {
		res, err := Import(ctx, db, []string{path},
			[]Option{WithSheets([]string{"keep", "drop"})}, []ImportOption{WithReplace()})
		if err != nil {
			t.Fatal(err)
		}
		assertStrings(t, "表清单", res[0].Tables, []string{"keep", "drop"})
	})

	t.Run("点名的同时跳过", func(t *testing.T) {
		// 这一条在修复前会把 drop 也导进来
		res, err := Import(ctx, db, []string{path},
			[]Option{WithSheets([]string{"keep", "drop"}), WithSkipSheets([]string{"drop"})},
			[]ImportOption{WithReplace()})
		if err != nil {
			t.Fatal(err)
		}
		assertStrings(t, "表清单", res[0].Tables, []string{"keep"})
	})

	t.Run("只跳过", func(t *testing.T) {
		res, err := Import(ctx, db, []string{path},
			[]Option{WithSkipSheets([]string{"drop"})}, []ImportOption{WithReplace()})
		if err != nil {
			t.Fatal(err)
		}
		assertStrings(t, "表清单", res[0].Tables, []string{"keep", "other"})
	})
}

// 点名的 Sheet 全都不存在 / 全被跳过时，错误信息里要列出可用的 Sheet 名，便于发现拼写错误。
func TestImportExcelSheetFilterAllMissed(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	path := writeXLSX(t, "miss.xlsx", []sheetSpec{
		{name: "alpha", rows: [][]string{{"a"}, {"1"}}},
		{name: "beta", rows: [][]string{{"b"}, {"2"}}},
	})

	for _, opts := range [][]Option{
		{WithSheets([]string{"typo"})},
		{WithSheets([]string{"alpha"}), WithSkipSheets([]string{"alpha"})},
	} {
		_, err := Import(ctx, db, []string{path}, opts, nil)
		if err == nil {
			t.Fatalf("应当报错，opts=%v", opts)
		}
		msg := err.Error()
		if !strings.Contains(msg, "没有符合条件的工作表") ||
			!strings.Contains(msg, "alpha") || !strings.Contains(msg, "beta") {
			t.Errorf("错误信息应列出可用 Sheet 名，得到: %v", err)
		}
	}
}

func TestImportExcelOnlyActiveSheet(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	path := writeXLSX(t, "active.xlsx", []sheetSpec{
		{name: "first", rows: [][]string{{"a"}, {"1"}}},
		{name: "second", rows: [][]string{{"b"}, {"2"}}},
	})

	res, err := Import(ctx, db, []string{path},
		[]Option{WithOnlyActiveSheet(true)}, []ImportOption{WithReplace()})
	if err != nil {
		t.Fatal(err)
	}
	if len(res[0].Tables) != 1 {
		t.Errorf("只处理活动 Sheet 时应只有一张表，得到 %v", res[0].Tables)
	}
}

func TestImportExcelHeaderRowAndRawHeader(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	path := writeXLSX(t, "raw.xlsx", []sheetSpec{
		{name: "raw", rows: [][]string{
			{"说明：某月数据"}, // 第 1 行不是表头
			{"PCI", "创建时间 (UTC)"},
			{"460-01", "2026-01-01"},
		}},
	})

	res, err := Import(ctx, db, []string{path},
		[]Option{WithHeaderRow(2), WithRawHeader()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// WithRawHeader：大小写与括号都保留
	assertStrings(t, "原始表头", res[0].Columns, []string{"PCI", "创建时间 (UTC)"})

	got := queryStrings(t, db, `SELECT "PCI" FROM raw`)
	assertStrings(t, "PCI 列", got, []string{"460-01"})
}

func TestImportExcelEmptySheet(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	t.Run("空表被跳过，其余照导", func(t *testing.T) {
		path := writeXLSX(t, "withempty.xlsx", []sheetSpec{
			{name: "data", rows: [][]string{{"a"}, {"1"}}},
			{name: "empty"},
		})
		res, err := Import(ctx, db, []string{path}, nil, []ImportOption{WithReplace()})
		if err != nil {
			t.Fatal(err)
		}
		assertStrings(t, "表清单", res[0].Tables, []string{"data"})
	})

	t.Run("全是空表时报错", func(t *testing.T) {
		path := writeXLSX(t, "onlyempty.xlsx", []sheetSpec{{name: "empty"}})
		if _, err := Import(ctx, db, []string{path}, nil, nil); err == nil {
			t.Fatal("全是空表时应当报错")
		} else if !strings.Contains(err.Error(), "空表") {
			t.Errorf("错误信息应说明没有数据: %v", err)
		}
	})
}

// 多 Sheet 时 WithTable 没有意义（会把结构不同的数据混进一张表），必须被忽略。
func TestImportExcelTableIgnoredForMultiSheet(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	path := writeXLSX(t, "multi2.xlsx", []sheetSpec{
		{name: "one", rows: [][]string{{"a"}, {"1"}}},
		{name: "two", rows: [][]string{{"b"}, {"2"}}},
	})

	res, err := Import(ctx, db, []string{path}, nil, []ImportOption{WithTable("mixed")})
	if err != nil {
		t.Fatal(err)
	}
	assertStrings(t, "表清单", res[0].Tables, []string{"one", "two"})

	tables, err := Tables(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range tables {
		if name == "mixed" {
			t.Error("多 Sheet 时不应生成 WithTable 指定的表")
		}
	}
}

// Sheet 名以数字开头时必须加 sheet_ 前缀，否则是非法标识符。
func TestImportExcelNumericSheetName(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	path := writeXLSX(t, "num.xlsx", []sheetSpec{
		{name: "2024", rows: [][]string{{"a"}, {"1"}}},
	})

	res, err := Import(ctx, db, []string{path}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res[0].Tables[0] != "sheet_2024" {
		t.Errorf("表名 = %q, want sheet_2024", res[0].Tables[0])
	}
}

func TestImportExcelMissingFile(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	if _, err := Import(ctx, db, []string{filepath.Join(t.TempDir(), "nope.xlsx")}, nil, nil); err == nil {
		t.Fatal("文件不存在应当报错")
	}
}

// 被破坏的 xlsx（不是合法 zip）要给出明确错误，而不是 panic。
func TestImportExcelBrokenFile(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	path := filepath.Join(t.TempDir(), "broken.xlsx")
	if err := os.WriteFile(path, []byte("not a xlsx"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Import(ctx, db, []string{path}, nil, nil); err == nil {
		t.Fatal("非法 xlsx 应当报错")
	}
}
