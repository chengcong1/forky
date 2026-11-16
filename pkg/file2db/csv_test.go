package file2db

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func TestImportCSVBasic(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	path := writeFile(t, "users.csv", []byte("Name,Age,备注\n张三,30,460-01\n李四,25,007\n"))
	res, err := Import(ctx, db, []string{path}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 {
		t.Fatalf("结果数 = %d, want 1", len(res))
	}
	r := res[0]
	if r.Table != "users" {
		t.Errorf("表名 = %q, want users（取文件名）", r.Table)
	}
	if r.Rows != 2 || r.Skipped != 0 {
		t.Errorf("Rows=%d Skipped=%d, want 2/0", r.Rows, r.Skipped)
	}
	// 列名默认清洗：Name -> name
	assertStrings(t, "列名", r.Columns, []string{"name", "age", "备注"})

	// 前导 0 必须原样保留（默认全 TEXT）
	got := queryStrings(t, db, `SELECT 备注 FROM users ORDER BY age`)
	assertStrings(t, "备注列", got, []string{"007", "460-01"})
}

func TestImportCSVGBKAutoDetect(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	gbk, err := simplifiedchinese.GB18030.NewEncoder().String("姓名,年龄\n张三,30\n")
	if err != nil {
		t.Fatal(err)
	}
	path := writeFile(t, "gbk.csv", []byte(gbk))

	if _, err := Import(ctx, db, []string{path}, nil, nil); err != nil {
		t.Fatal(err)
	}
	got := queryStrings(t, db, `SELECT 姓名 FROM gbk`)
	assertStrings(t, "GBK 中文列", got, []string{"张三"})
}

func TestImportCSVHeaderRowAndSkipRows(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	// 第 1 行是标题、第 2 行是表头、第 3 行是说明行，数据从第 4 行开始
	path := writeFile(t, "report.csv", []byte(
		"2026 年报表\nname,age\n单位：人\n张三,30\n李四,25\n"))

	res, err := Import(ctx, db, []string{path},
		[]Option{WithHeaderRow(2), WithSkipRows(1)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertStrings(t, "列名", res[0].Columns, []string{"name", "age"})
	if res[0].Rows != 2 {
		t.Errorf("Rows = %d, want 2", res[0].Rows)
	}
	assertStrings(t, "name 列", queryStrings(t, db, `SELECT name FROM report`), []string{"张三", "李四"})
}

func TestImportCSVNoHeader(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	path := writeFile(t, "raw.csv", []byte("张三,30\n李四,25\n"))
	res, err := Import(ctx, db, []string{path}, []Option{WithNoHeader()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertStrings(t, "列名", res[0].Columns, []string{"col1", "col2"})
	if res[0].Rows != 2 {
		t.Errorf("Rows = %d, want 2（第一行也是数据）", res[0].Rows)
	}
}

// 行尾缺列要补空串、多余列要截断，否则会报 missing argument with index N。
func TestImportCSVRaggedRows(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	path := writeFile(t, "ragged.csv", []byte("a,b,c\n1\n1,2,3,4\n"))
	if _, err := Import(ctx, db, []string{path}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, db, "ragged"); n != 2 {
		t.Fatalf("表里 %d 行, want 2", n)
	}
	got := queryStrings(t, db, `SELECT a || '|' || b || '|' || c FROM ragged ORDER BY id`)
	assertStrings(t, "补齐后的行", got, []string{"1||", "1|2|3"})
}

func TestImportCSVBlankRowSkipped(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	// 中间那行的两个字段都是空串，应被跳过并计入 Skipped
	path := writeFile(t, "blank.csv", []byte("a,b\n1,2\n,\n3,4\n"))
	res, err := Import(ctx, db, []string{path}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res[0].Rows != 2 || res[0].Skipped != 1 {
		t.Errorf("Rows=%d Skipped=%d, want 2/1", res[0].Rows, res[0].Skipped)
	}
}

func TestImportCSVKeepBlankRows(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	path := writeFile(t, "blank2.csv", []byte("a,b\n1,2\n,\n3,4\n"))
	res, err := Import(ctx, db, []string{path}, nil, []ImportOption{WithKeepBlankRows()})
	if err != nil {
		t.Fatal(err)
	}
	if res[0].Rows != 3 || res[0].Skipped != 0 {
		t.Errorf("Rows=%d Skipped=%d, want 3/0", res[0].Rows, res[0].Skipped)
	}
}

func TestImportTSVAndCustomDelimiter(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	tsv := writeFile(t, "a.tsv", []byte("name\tage\n张三\t30\n"))
	sc := writeFile(t, "b.csv", []byte("name;age\n李四;25\n"))

	if _, err := Import(ctx, db, []string{tsv}, nil, nil); err != nil {
		t.Fatalf("TSV 导入失败: %v", err)
	}
	if _, err := Import(ctx, db, []string{sc},
		[]Option{WithDelimiter(';')}, nil); err != nil {
		t.Fatalf("分号分隔导入失败: %v", err)
	}

	assertStrings(t, "TSV", queryStrings(t, db, `SELECT name FROM a`), []string{"张三"})
	assertStrings(t, "分号 CSV", queryStrings(t, db, `SELECT name FROM b`), []string{"李四"})
}

func TestImportCSVTableNameOptions(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	path := writeFile(t, "用户 数据.csv", []byte("a\n1\n"))

	// 默认取文件名并安全化
	res, err := Import(ctx, db, []string{path}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res[0].Table != "用户_数据" {
		t.Errorf("默认表名 = %q, want 用户_数据", res[0].Table)
	}

	// WithTable 优先
	path2 := writeFile(t, "other.csv", []byte("a\n2\n"))
	res, err = Import(ctx, db, []string{path2}, nil, []ImportOption{WithTable("custom")})
	if err != nil {
		t.Fatal(err)
	}
	if res[0].Table != "custom" {
		t.Errorf("WithTable 表名 = %q, want custom", res[0].Table)
	}
}

func TestImportCSVEmptyFile(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	path := writeFile(t, "empty.csv", nil)
	if _, err := Import(ctx, db, []string{path}, nil, nil); err == nil {
		t.Fatal("空文件应当报错")
	} else if !strings.Contains(err.Error(), "没有可导入的数据") {
		t.Errorf("错误信息应说明没有数据: %v", err)
	}
}

func TestImportMissingFile(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	_, err := Import(ctx, db, []string{filepath.Join(t.TempDir(), "nope.csv")}, nil, nil)
	if err == nil {
		t.Fatal("文件不存在应当报错")
	}
}

// 进度日志每 N 行打一条，方便大文件导入时观察进度。
func TestImportProgressLogger(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	var buf strings.Builder
	lg := newTestLogger(&buf)

	path := writeFile(t, "p.csv", []byte("a\n1\n2\n3\n4\n5\n"))
	if _, err := Import(ctx, db, []string{path}, nil,
		[]ImportOption{WithLogger(lg), WithProgress(2)}); err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	if !strings.Contains(out, "已写入 2 行") || !strings.Contains(out, "已写入 4 行") {
		t.Errorf("进度日志内容不对:\n%s", out)
	}
	if strings.Contains(out, "已写入 5 行") {
		t.Errorf("最后不满 2 行时不应打进度:\n%s", out)
	}
}

func TestCSVSourceEncodingAndClose(t *testing.T) {
	gbk, err := simplifiedchinese.GB18030.NewEncoder().String("a\n中文\n")
	if err != nil {
		t.Fatal(err)
	}
	path := writeFile(t, "e.csv", []byte(gbk))

	src, err := NewCSV(path)
	if err != nil {
		t.Fatal(err)
	}
	if src.Encoding() != EncodingGB18030 {
		t.Errorf("识别到的编码 = %q, want %q", src.Encoding(), EncodingGB18030)
	}
	if err := src.Close(); err != nil {
		t.Errorf("Close 报错: %v", err)
	}
	// 关两次不能 panic
	if err := src.Close(); err != nil {
		t.Errorf("重复 Close 不应报错: %v", err)
	}
}
