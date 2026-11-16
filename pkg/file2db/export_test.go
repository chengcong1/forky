package file2db

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
	"golang.org/x/text/encoding/simplifiedchinese"
)

// seedExportDB 造一张用于导出测试的表：users(name, note)。
func seedExportDB(t *testing.T) *DB {
	t.Helper()
	ctx := context.Background()
	db := newTestDB(t)

	path := writeFile(t, "seed.csv", []byte("name,note\n张三,007\n李四,460-01\n"))
	if _, err := Import(ctx, db, []string{path}, nil, []ImportOption{WithTable("users")}); err != nil {
		t.Fatalf("准备测试数据失败: %v", err)
	}
	return db
}

func TestExportTableCSV(t *testing.T) {
	ctx := context.Background()
	db := seedExportDB(t)
	out := filepath.Join(t.TempDir(), "users.csv")

	res, err := ExportTable(ctx, db, "users", out, WithExportIndex(false))
	if err != nil {
		t.Fatal(err)
	}
	if res.Table != "users" || res.Rows != 2 {
		t.Errorf("导出结果 = {%s, %d 行}, want {users, 2 行}", res.Table, res.Rows)
	}
	assertStrings(t, "导出列", res.Columns, []string{"name", "note"})

	rows := readCSV(t, out)
	assertStrings(t, "表头", rows[0], []string{"name", "note"})
	assertStrings(t, "第一行", rows[1], []string{"张三", "007"})
	assertStrings(t, "第二行", rows[2], []string{"李四", "460-01"})
	if len(rows) != 3 {
		t.Errorf("CSV 共 %d 行（含表头）, want 3", len(rows))
	}
}

func TestExportTableCSVWithIndex(t *testing.T) {
	ctx := context.Background()
	db := seedExportDB(t)
	out := filepath.Join(t.TempDir(), "with-id.csv")

	res, err := ExportTable(ctx, db, "users", out) // 默认导出主键列
	if err != nil {
		t.Fatal(err)
	}
	assertStrings(t, "导出列", res.Columns, []string{"id", "name", "note"})
	assertStrings(t, "表头", readCSV(t, out)[0], []string{"id", "name", "note"})
}

// 数字、带前导 0 的编号必须按字符串写出，不能被当成数字改格式。
func TestExportPreservesText(t *testing.T) {
	ctx := context.Background()
	db := seedExportDB(t)
	out := filepath.Join(t.TempDir(), "t.csv")

	if _, err := ExportTable(ctx, db, "users", out, WithExportIndex(false)); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte("007")) {
		t.Errorf("导出内容丢了前导 0:\n%s", raw)
	}
}

func TestExportCSVEncodingAndBOM(t *testing.T) {
	ctx := context.Background()
	db := seedExportDB(t)
	dir := t.TempDir()

	t.Run("gbk 中文可解码", func(t *testing.T) {
		out := filepath.Join(dir, "gbk.csv")
		if _, err := ExportTable(ctx, db, "users", out,
			WithExportIndex(false), WithExportEncoding(EncodingGBK)); err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		text, err := simplifiedchinese.GB18030.NewDecoder().String(string(raw))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(text, "张三") {
			t.Errorf("GBK 导出解码后没有中文:\n%s", text)
		}
	})

	t.Run("utf-8 带 BOM", func(t *testing.T) {
		out := filepath.Join(dir, "bom.csv")
		if _, err := ExportTable(ctx, db, "users", out,
			WithExportIndex(false), WithExportEncoding(EncodingUTF8), WithExportBOM()); err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.HasPrefix(raw, []byte{0xEF, 0xBB, 0xBF}) {
			t.Errorf("应写入 UTF-8 BOM，实际开头: % X", raw[:3])
		}
	})
}

func TestExportDelimiter(t *testing.T) {
	ctx := context.Background()
	db := seedExportDB(t)
	out := filepath.Join(t.TempDir(), "semi.csv")

	if _, err := ExportTable(ctx, db, "users", out,
		WithExportIndex(false), WithExportDelimiter(';')); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte("name;note")) {
		t.Errorf("分隔符没生效:\n%s", raw)
	}
}

func TestExportTableXLSX(t *testing.T) {
	ctx := context.Background()
	db := seedExportDB(t)
	out := filepath.Join(t.TempDir(), "users.xlsx")

	res, err := ExportTable(ctx, db, "users", out, WithExportIndex(false))
	if err != nil {
		t.Fatal(err)
	}
	if res.Sheet != "users" {
		t.Errorf("Sheet 名 = %q, want users", res.Sheet)
	}

	rows := readXLSX(t, out, "users")
	assertStrings(t, "表头", rows[0], []string{"name", "note"})
	assertStrings(t, "第一行", rows[1], []string{"张三", "007"})
	if len(rows) != 3 {
		t.Errorf("xlsx 共 %d 行, want 3", len(rows))
	}
}

func TestExportTablesToDir(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	for _, name := range []string{"t1", "t2"} {
		path := writeFile(t, name+".csv", []byte("a\n1\n"))
		if _, err := Import(ctx, db, []string{path}, nil, []ImportOption{WithTable(name)}); err != nil {
			t.Fatal(err)
		}
	}

	dir := filepath.Join(t.TempDir(), "out")
	results, err := ExportTables(ctx, db, dir, WithExportIndex(false))
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("导出 %d 张表, want 2", len(results))
	}
	for _, name := range []string{"t1.csv", "t2.csv"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("缺少产物 %s: %v", name, err)
		}
	}
}

func TestExportTablesSingleFile(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	for _, name := range []string{"t1", "t2"} {
		path := writeFile(t, name+".csv", []byte("a\n1\n"))
		if _, err := Import(ctx, db, []string{path}, nil, []ImportOption{WithTable(name)}); err != nil {
			t.Fatal(err)
		}
	}

	dir := filepath.Join(t.TempDir(), "out")
	out := filepath.Join(dir, "all.xlsx")
	if _, err := ExportTables(ctx, db, dir,
		WithExportSingleFile("all.xlsx"), WithExportFormat(FormatXLSX), WithExportIndex(false)); err != nil {
		t.Fatal(err)
	}

	f, err := excelize.OpenFile(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	sheets := f.GetSheetList()
	if len(sheets) != 2 {
		t.Fatalf("单文件里 %d 个 Sheet, want 2（%v）", len(sheets), sheets)
	}
	for _, name := range []string{"t1", "t2"} {
		found := false
		for _, s := range sheets {
			if s == name {
				found = true
			}
		}
		if !found {
			t.Errorf("单文件里缺少 Sheet %q（实际 %v）", name, sheets)
		}
	}
}

// 单文件模式只支持 xlsx：传 .csv 必须报错，而不是悄悄只写一张表。
func TestExportSingleFileRejectsCSV(t *testing.T) {
	ctx := context.Background()
	db := seedExportDB(t)

	_, err := ExportTables(ctx, db, t.TempDir(), WithExportSingleFile("all.csv"))
	if err == nil {
		t.Fatal("单文件 + csv 应当报错")
	}
	if !strings.Contains(err.Error(), "只支持 xlsx") {
		t.Errorf("错误信息不明确: %v", err)
	}
}

func TestExportSlice(t *testing.T) {
	ctx := context.Background()
	db := seedExportDB(t)

	rows, err := ExportSlice(ctx, db, "users", WithExportIndex(false))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("切块 %d 行（含表头）, want 3", len(rows))
	}
	assertStrings(t, "表头", rows[0], []string{"name", "note"})
	assertStrings(t, "第一行", rows[1], []string{"张三", "007"})
}

func TestExportNullAsEmpty(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	if _, err := db.Exec(`CREATE TABLE t (a TEXT, b TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO t (a, b) VALUES ('x', NULL)`); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(t.TempDir(), "null.csv")
	if _, err := ExportTable(ctx, db, "t", out, WithExportIndex(false)); err != nil {
		t.Fatal(err)
	}
	rows := readCSV(t, out)
	assertStrings(t, "NULL 应写成空串", rows[1], []string{"x", ""})

	// 内存切片导出也一样（不会出现 converting NULL to string is unsupported）
	slice, err := ExportSlice(ctx, db, "t", WithExportIndex(false))
	if err != nil {
		t.Fatal(err)
	}
	assertStrings(t, "切片导出", slice[1], []string{"x", ""})
}

func TestExportTableNotExist(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	_, err := ExportTable(ctx, db, "nope", filepath.Join(t.TempDir(), "a.csv"))
	if err == nil {
		t.Fatal("表不存在应当报错")
	}
	if !strings.Contains(err.Error(), "不存在") {
		t.Errorf("错误信息不明确: %v", err)
	}
}

// 取消后不能留下半截产物。
func TestExportCancelledLeavesNoFile(t *testing.T) {
	db := seedExportDB(t)
	out := filepath.Join(t.TempDir(), "cancelled.csv")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := ExportTable(ctx, db, "users", out, WithExportIndex(false))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("错误应为 context.Canceled，得到 %v", err)
	}
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Errorf("取消后不应留下文件，stat err = %v", statErr)
	}
}

func TestExportTablesEmptyDB(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	if _, err := ExportTables(ctx, db, filepath.Join(t.TempDir(), "out")); err == nil {
		t.Fatal("库里没有表时应当报错")
	}
}

// ExportTable 不会自动创建输出目录（ExportTables 会），这个行为要明确。
func TestExportTableMissingDir(t *testing.T) {
	ctx := context.Background()
	db := seedExportDB(t)

	// CSV：创建文件时就失败
	csvOut := filepath.Join(t.TempDir(), "no-such-dir", "a.csv")
	if _, err := ExportTable(ctx, db, "users", csvOut); err == nil {
		t.Fatal("输出目录不存在时应当报错（csv）")
	}

	// xlsx：写完全部行才在保存时失败，半截文件不能留下
	xlsxOut := filepath.Join(t.TempDir(), "no-such-dir", "a.xlsx")
	if _, err := ExportTable(ctx, db, "users", xlsxOut); err == nil {
		t.Fatal("输出目录不存在时应当报错（xlsx）")
	}
	if _, statErr := os.Stat(xlsxOut); !os.IsNotExist(statErr) {
		t.Errorf("失败后不应留下文件，stat err = %v", statErr)
	}
}

// ------------------------------ 小工具 ------------------------------

// readCSV 按 UTF-8 读回导出的 CSV（含表头行）。
func readCSV(t *testing.T, path string) [][]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("打开 %s 失败: %v", path, err)
	}
	defer f.Close()

	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatalf("解析 CSV %s 失败: %v", path, err)
	}
	return rows
}

// readXLSX 读回指定 Sheet 的所有行。
func readXLSX(t *testing.T, path, sheet string) [][]string {
	t.Helper()
	f, err := excelize.OpenFile(path)
	if err != nil {
		t.Fatalf("打开 xlsx 失败: %v", err)
	}
	defer f.Close()

	rows, err := f.GetRows(sheet)
	if err != nil {
		t.Fatalf("读取 Sheet %q 失败: %v", sheet, err)
	}
	return rows
}
