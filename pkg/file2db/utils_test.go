package file2db

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSanitizeHeader(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"name", "name"},
		{"PCI", "pci"},
		{"  Name  ", "name"},
		{"\ufeffname", "name"},      // 残留 BOM
		{"创建时间 (UTC)", "创建时间__utc"}, // 空格与括号都变下划线，首尾下划线去掉
		{"a-b", "a_b"},              // 短横线
		{"a.b/c\\d", "a_b_c_d"},     // 点、斜杠、反斜杠
		{"（备注）", "备注"},              // 全角括号映射成下划线后被 trim 掉
		{"_已有下划线_", "已有下划线"},        // 首尾下划线去掉
		{"小区 编号", "小区_编号"},          // 中文与空格
		{"", ""},                    // 空串保持空，由 CleanHeaders 兜底
	}
	for _, c := range cases {
		if got := SanitizeHeader(c.in); got != c.want {
			t.Errorf("SanitizeHeader(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRawHeader(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"  PCI  ", "PCI"}, // 大小写与内容不动
		{"创建时间 (UTC)", "创建时间 (UTC)"},
		{"\ufeffname", "name"},
		{"a-b", "a-b"},
	}
	for _, c := range cases {
		if got := RawHeader(c.in); got != c.want {
			t.Errorf("RawHeader(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCleanHeadersDefault(t *testing.T) {
	got := CleanHeaders([]string{"PCI", "Age", "age", "创建时间 (UTC)", "", "id"}, false)
	want := []string{"pci", "age", "age_2", "创建时间__utc", "col5", "id_2"}
	assertStrings(t, "CleanHeaders(default)", got, want)
}

func TestCleanHeadersRawKeepsOriginal(t *testing.T) {
	got := CleanHeaders([]string{"PCI", "创建时间 (UTC)", "ID"}, true)
	// 只有大小写/标点原样保留；与自增主键 id 同名（大小写不敏感）仍要让位
	want := []string{"PCI", "创建时间 (UTC)", "ID_2"}
	assertStrings(t, "CleanHeaders(raw)", got, want)
}

// 列名去重必须按 SQLite 的规则（标识符大小写不敏感），并且能跳过已被占用的名字。
func TestCleanHeadersDedupeIsCaseInsensitive(t *testing.T) {
	got := CleanHeaders([]string{"name", "Name", "NAME", "name_2"}, false)
	want := []string{"name", "name_2", "name_3", "name_2_2"}
	assertStrings(t, "CleanHeaders(dedupe)", got, want)
}

func TestCleanHeadersEmptyFallsBackToColN(t *testing.T) {
	got := CleanHeaders([]string{"", "", "col1"}, false)
	// 第 3 个 "col1" 与自动生成的第 1 个重名，必须让位
	want := []string{"col1", "col2", "col1_2"}
	assertStrings(t, "CleanHeaders(empty)", got, want)
}

func TestSanitizeTableName(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"users", "users"},
		{"2024-users (v2)", "sheet_2024_users__v2"}, // 数字开头加前缀，非法字符变下划线
		{"", "data"},
		{"   ", "data"},
		{"!!!", "data"},
		{"表 1", "表_1"},       // 中文保留
		{"Sheet1", "Sheet1"}, // 大小写不动
	}
	for _, c := range cases {
		if got := SanitizeTableName(c.in); got != c.want {
			t.Errorf("SanitizeTableName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestTableNameFromPath(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"/a/b/users.csv", "users"},
		{`C:\data\用户 列表.xlsx`, "用户_列表"},
		{"data-1.csv", "data_1"},
		{"!!!.csv", "data"},
		{"noext", "noext"},
	}
	for _, c := range cases {
		if got := TableNameFromPath(c.in); got != c.want {
			t.Errorf("TableNameFromPath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestAutoHeaderAndBlankRow(t *testing.T) {
	assertStrings(t, "autoHeader(3)", autoHeader(3), []string{"col1", "col2", "col3"})
	assertStrings(t, "autoHeader(0)", autoHeader(0), []string{})

	if !isBlankRow(nil) || !isBlankRow([]string{"", "  ", "\t"}) {
		t.Error("isBlankRow 应该把空串/空白当空行")
	}
	if isBlankRow([]string{"", "x"}) {
		t.Error("isBlankRow 遇到非空单元格应返回 false")
	}
}

func TestNormalizeRow(t *testing.T) {
	got := normalizeRow([]string{"a", "b"}, 4)
	want := []any{"a", "b", "", ""}
	if len(got) != len(want) {
		t.Fatalf("normalizeRow 补齐后长度 = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("normalizeRow[%d] = %v, want %v", i, got[i], want[i])
		}
	}
	// 多余列要截断
	if got := normalizeRow([]string{"a", "b", "c"}, 2); len(got) != 2 {
		t.Errorf("normalizeRow 截断后长度 = %d, want 2", len(got))
	}
}

func TestResolveIndexColumns(t *testing.T) {
	headers := []string{"AGE", "Name", "创建时间 (UTC)"}

	got := resolveIndexColumns(headers, []string{"age", "NAME", "missing"})
	assertStrings(t, "resolveIndexColumns(大小写不敏感)", got, []string{"AGE", "Name"})

	// 原始表头与清洗后的列名都能命中，重复的只保留一次
	got = resolveIndexColumns(headers, []string{"创建时间 (UTC)", "创建时间__utc", "age", "AGE"})
	assertStrings(t, "resolveIndexColumns(去重)", got, []string{"创建时间 (UTC)", "AGE"})

	if got := resolveIndexColumns(headers, []string{"nope"}); len(got) != 0 {
		t.Errorf("对不上的列应返回空切片，得到 %v", got)
	}
}

func TestDefaultIndexNameLengthAndUniqueness(t *testing.T) {
	long := strings.Repeat("列", 40)
	name := defaultIndexName("表"+long, []string{long, "age"})

	if len(name) > 60 {
		t.Errorf("索引名超过 60 字节：%d", len(name))
	}
	if !utf8.ValidString(name) {
		t.Error("索引名截断后不是合法 UTF-8")
	}
	if !regexp.MustCompile(`_[0-9a-f]{8}$`).MatchString(name) {
		t.Errorf("超长索引名应以 _<crc32> 结尾，得到 %q", name)
	}

	// 不同的长名字截断后必须仍然不同（CRC32 后缀就是为此）
	other := defaultIndexName("表"+long, []string{long, "sex"})
	if name == other {
		t.Error("两个不同索引截断后撞名了")
	}

	// 短名字不加后缀
	if got := defaultIndexName("t", []string{"age"}); got != "idx_t_age" {
		t.Errorf("defaultIndexName 短名字 = %q, want idx_t_age", got)
	}
}

func TestTruncateBytesKeepsRuneBoundary(t *testing.T) {
	s := "中文abc"
	for _, limit := range []int{0, 1, 2, 3, 4, 5, 6, 7, 100} {
		got := truncateBytes(s, limit)
		if !utf8.ValidString(got) {
			t.Errorf("truncateBytes(%q, %d) = %q 不是合法 UTF-8", s, limit, got)
		}
		if len(got) > limit && limit >= 0 {
			t.Errorf("truncateBytes(%q, %d) 长度 %d 超过上限", s, limit, len(got))
		}
	}
	if got := truncateBytes(s, 3); got != "中" {
		t.Errorf("truncateBytes 按字节 3 截断 = %q, want 中", got)
	}
}

func TestSafeFileName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"users", "users"},
		{`a<b>c:d"e/f\g|h?i*j`, "a_b_c_d_e_f_g_h_i_j"},
		{"表\x01名", "表_名"},
		{"   ", "table"},
		{"", "table"},
	}
	for _, c := range cases {
		if got := safeFileName(c.in); got != c.want {
			t.Errorf("safeFileName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSheetName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"users", "users"},
		{"a:b\\c/d?e*f[g]h", "a_b_c_d_e_f_g_h"},
		{"'quoted'", "quoted"},
		{"", "Sheet1"},
		{"   ", "Sheet1"},
		{strings.Repeat("x", 40), strings.Repeat("x", maxSheetName)},
	}
	for _, c := range cases {
		if got := sheetName(c.in); got != c.want {
			t.Errorf("sheetName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	// 多字节字符按字符数截断，不能切坏
	if got := sheetName(strings.Repeat("中", 40)); len([]rune(got)) != maxSheetName || !utf8.ValidString(got) {
		t.Errorf("sheetName 中文截断异常：%q（%d 字符）", got, len([]rune(got)))
	}
}

func TestResolveExportFormat(t *testing.T) {
	cases := []struct {
		format ExportFormat
		path   string
		want   ExportFormat
	}{
		{FormatXLSX, "a.csv", FormatXLSX}, // 显式指定优先
		{"", "a.xlsx", FormatXLSX},
		{"", "a.XLSM", FormatXLSX},
		{"", "a.csv", FormatCSV},
		{"", "", FormatCSV},
	}
	for _, c := range cases {
		if got := resolveExportFormat(c.format, c.path); got != c.want {
			t.Errorf("resolveExportFormat(%q, %q) = %q, want %q", c.format, c.path, got, c.want)
		}
	}
}

func TestExportColumns(t *testing.T) {
	cols := []tableColumn{{Name: "id", PK: true}, {Name: "name"}, {Name: "age"}}

	assertStrings(t, "exportColumns(带主键)", exportColumns(cols, true), []string{"id", "name", "age"})
	assertStrings(t, "exportColumns(不带主键)", exportColumns(cols, false), []string{"name", "age"})
}

func TestAnyToString(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{nil, ""}, // NULL 统一写空串
		{"abc", "abc"},
		{[]byte("bytes"), "bytes"},
		{int64(-12), "-12"},
		{float64(1.5), "1.5"},
		{true, "true"},
		{int(7), "7"}, // 非 int64 走 fmt
	}
	for _, c := range cases {
		if got := anyToString(c.in); got != c.want {
			t.Errorf("anyToString(%#v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// assertStrings 按元素比较两个字符串切片。
func assertStrings(t *testing.T, label string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: 长度 %d, want %d（got=%v）", label, len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%s: [%d] = %q, want %q（got=%v）", label, i, got[i], want[i], got)
		}
	}
}

// atoi 让测试里的行数断言更短。
func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
