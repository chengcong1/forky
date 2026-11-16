package file2db

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectKind(t *testing.T) {
	cases := []struct {
		path    string
		want    SourceKind
		wantErr bool
	}{
		{"a.xlsx", KindExcel, false},
		{"a.XLSM", KindExcel, false},
		{"a.xltx", KindExcel, false},
		{"a.csv", KindCSV, false},
		{"a.CSV", KindCSV, false},
		{"a.tsv", KindTSV, false},
		{"a.tab", KindTSV, false},
		{"a.txt", KindCSV, false}, // 纯文本按 CSV 处理
		{"a.json", "", true},
		{"a", "", true},
	}
	for _, c := range cases {
		got, err := DetectKind(c.path)
		if c.wantErr {
			if err == nil {
				t.Errorf("DetectKind(%q) 应当报错", c.path)
			}
			continue
		}
		if err != nil {
			t.Errorf("DetectKind(%q) 报错: %v", c.path, err)
			continue
		}
		if got != c.want {
			t.Errorf("DetectKind(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

// 批量创建数据源时，任一文件失败都要把已打开的关掉（否则文件句柄泄漏），并返回错误。
func TestNewSourcesClosesOpenedOnError(t *testing.T) {
	dir := t.TempDir()
	ok := filepath.Join(dir, "a.csv")
	if err := os.WriteFile(ok, []byte("a\n1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	sources, err := NewSources([]string{ok, filepath.Join(dir, "b.json")})
	if err == nil {
		t.Fatal("含不支持的文件类型时应返回错误")
	}
	if sources != nil {
		t.Errorf("失败时应返回 nil 数据源，得到 %d 个", len(sources))
	}
	if !strings.Contains(err.Error(), "不支持的文件类型") {
		t.Errorf("错误信息不够明确: %v", err)
	}
}

func TestReadHeaderRows(t *testing.T) {
	cases := []struct {
		name        string
		rows        [][]string
		opt         Option
		wantHeader  []string
		wantNext    []string // readHeaderRows 之后 next() 应返回的第一行数据
		wantPending []string
	}{
		{
			name:       "表头之前的空行不计入行数",
			rows:       [][]string{{"", ""}, {"name", "age"}, {"张三", "30"}},
			wantHeader: []string{"name", "age"},
			wantNext:   []string{"张三", "30"},
		},
		{
			name:       "HeaderRow=2 取第二个非空行",
			rows:       [][]string{{"说明", ""}, {"name", "age"}, {"张三", "30"}},
			opt:        WithHeaderRow(2),
			wantHeader: []string{"name", "age"},
			wantNext:   []string{"张三", "30"},
		},
		{
			name:       "SkipRows 按物理行跳过表头后的说明行",
			rows:       [][]string{{"name", "age"}, {"小计", ""}, {"张三", "30"}},
			opt:        WithSkipRows(1),
			wantHeader: []string{"name", "age"},
			wantNext:   []string{"张三", "30"},
		},
		{
			name:        "无表头模式：第一行就是数据",
			rows:        [][]string{{"张三", "30"}, {"李四", "25"}},
			opt:         WithNoHeader(),
			wantHeader:  []string{"col1", "col2"},
			wantPending: []string{"张三", "30"},
			wantNext:    []string{"李四", "25"},
		},
		{
			name: "全是空行",
			rows: [][]string{{"", ""}, {"  ", "\t"}},
		},
		{
			name: "空数据源",
			rows: nil,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := defaultReadConfig()
			if c.opt != nil {
				c.opt(&cfg)
			}
			next := readerOf(c.rows)

			header, pending, err := readHeaderRows(next, cfg)
			if err != nil {
				t.Fatalf("readHeaderRows 报错: %v", err)
			}
			assertStrings(t, "header", header, c.wantHeader)
			assertStrings(t, "pending", pending, c.wantPending)

			if c.wantNext != nil {
				got, err := next()
				if err != nil {
					t.Fatalf("读取下一行失败: %v", err)
				}
				assertStrings(t, "next", got, c.wantNext)
			}
		})
	}
}

// SkipRows 跳过表头后面的行时如果提前 EOF，不能 panic，也不能把 header 丢掉。
func TestReadHeaderRowsSkipRowsHitsEOF(t *testing.T) {
	cfg := defaultReadConfig()
	WithSkipRows(5)(&cfg)

	header, pending, err := readHeaderRows(readerOf([][]string{{"a", "b"}}), cfg)
	if err != nil {
		t.Fatalf("提前 EOF 不应报错: %v", err)
	}
	assertStrings(t, "header", header, []string{"a", "b"})
	if pending != nil {
		t.Errorf("pending 应为 nil，得到 %v", pending)
	}
}

func TestHandleUseSheet(t *testing.T) {
	all := []string{"a", "b", "c"}

	cases := []struct {
		name string
		use  []string
		skip []string
		want []string
	}{
		{"都没给取全部", nil, nil, []string{"a", "b", "c"}},
		{"只点名", []string{"a"}, nil, []string{"a"}},
		{"只跳过", nil, []string{"b"}, []string{"a", "c"}},
		{"点名且跳过：先取交集再减", []string{"a", "b"}, []string{"b"}, []string{"a"}},
		{"点名不存在的", []string{"nope"}, nil, nil},
		{"都跳过", nil, []string{"a", "b", "c"}, nil},
		{"结果按工作簿顺序而不是点名顺序", []string{"c", "a"}, nil, []string{"a", "c"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := handleUseSheet(all, c.use, c.skip)
			if len(got) != len(c.want) {
				t.Fatalf("handleUseSheet = %v, want %v", got, c.want)
			}
			for i := range c.want {
				if got[i] != c.want[i] {
					t.Fatalf("handleUseSheet = %v, want %v", got, c.want)
				}
			}
		})
	}
}

func TestSliceSource(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	src := NewSlice([][]string{
		{"name", "age"},
		{"张三", "30"},
		{"李四", "25"},
	})
	if src.Len() != 3 {
		t.Errorf("Len = %d, want 3（含表头）", src.Len())
	}
	if err := src.Close(); err != nil {
		t.Errorf("Close 应返回 nil，得到 %v", err)
	}

	res, err := src.Import(ctx, db, WithTable("people"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Table != "people" || res.Rows != 2 {
		t.Errorf("导入结果 = {%s, %d 行}, want {people, 2 行}", res.Table, res.Rows)
	}
	if n := countRows(t, db, "people"); n != 2 {
		t.Errorf("表里 %d 行, want 2", n)
	}

	// 同一个数据源可以重复导入：Reset 后从头再读
	src.Reset()
	res, err = src.Import(ctx, db, WithTable("people"), WithReplace())
	if err != nil {
		t.Fatal(err)
	}
	if res.Rows != 2 || countRows(t, db, "people") != 2 {
		t.Errorf("重复导入后行数不对：res=%d, 表里=%d", res.Rows, countRows(t, db, "people"))
	}
}

func TestSliceSourceDefaultTableAndNoHeader(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	// 不指定表名时兜底 data；没有表头时列名自动 col1..colN
	src := NewSlice([][]string{{"张三", "30"}}, WithNoHeader())
	res, err := src.Import(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if res.Table != "data" {
		t.Errorf("默认表名 = %q, want data", res.Table)
	}
	assertStrings(t, "列名", res.Columns, []string{"col1", "col2"})
	if n := countRows(t, db, "data"); n != 1 {
		t.Errorf("表里 %d 行, want 1", n)
	}
}

func TestSliceSourceEmpty(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	for _, rows := range [][][]string{nil, {{"", " "}}} {
		src := NewSlice(rows)
		if _, err := src.Import(ctx, db); err == nil {
			t.Fatalf("空切片 %v 应当报错", rows)
		}
	}
}

func TestSourceInterfaceSatisfied(t *testing.T) {
	// 编译期断言在 source.go 里已有；这里补一个运行期确认，避免接口被无意改窄
	var _ Source = NewSlice(nil)
}

// readerOf 把二维切片包成 rowReader，读完返回 io.EOF。
func readerOf(rows [][]string) rowReader {
	i := 0
	return func() ([]string, error) {
		if i >= len(rows) {
			return nil, io.EOF
		}
		row := rows[i]
		i++
		return row, nil
	}
}
