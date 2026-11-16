package file2db

import (
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
)

// ---------- 列名 / 表名清洗 ----------

// trimHeader 去掉表头首尾空白，并清掉可能残留的 BOM。
func trimHeader(h string) string {
	h = strings.TrimSpace(h)
	h = strings.TrimPrefix(h, "\ufeff")
	return strings.TrimSpace(h)
}

// SanitizeHeader 清洗单个列名：去掉空白与 BOM、统一小写、空格与常见分隔符替换为下划线。
func SanitizeHeader(h string) string {
	h = trimHeader(h)
	h = strings.ToLower(h)
	h = strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', '\n', '\r', '-', '.', '/', '\\', '（', '）', '(', ')':
			return '_'
		}
		return r
	}, h)
	h = strings.Trim(h, "_")
	return h
}

// RawHeader 保留原始列名：只去掉首尾空白与 BOM，
// 不改大小写、也不动空格 / 括号 / 连字符等字符（写 SQL 时用双引号包裹即可）。
func RawHeader(h string) string {
	return trimHeader(h)
}

// SanitizeHeaders 批量清洗列名（默认模式），等价于 CleanHeaders(headers, false)。
func SanitizeHeaders(headers []string) []string {
	return CleanHeaders(headers, false)
}

// asciiFold 按 SQLite 的标识符比较规则折叠大小写：只对 ASCII 字母生效。
// 用于判断两个列名在建表时是否算“同一个名字”。
func asciiFold(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return r + ('a' - 'A')
		}
		return r
	}, s)
}

// CleanHeaders 批量处理列名，keepRaw 决定单个列名走哪套规则：
//   - false（默认）：用 SanitizeHeader，小写化 + 标点转下划线，便于写 SQL；
//   - true：用 RawHeader 保留原始表头，只去首尾空白与 BOM。
//
// 两种模式都会保证建表不会因列名失败：
//   - 空列名自动补成 colN；
//   - 重名列追加 _2、_3 后缀（会跳过已被占用的名字）；
//   - 与自增主键同名的列会被改名（主键固定叫 id）。
//
// 重名按 SQLite 的规则判断：标识符大小写不敏感，所以 "ID" 与 "id"、
// "Age" 与 "age" 也算重名。
func CleanHeaders(headers []string, keepRaw bool) []string {
	out := make([]string, len(headers))
	// 预占主键名：buildCreateTableSQL 里固定建了一列 id INTEGER PRIMARY KEY
	seen := map[string]struct{}{"id": {}}
	for i, h := range headers {
		name := SanitizeHeader(h)
		if keepRaw {
			name = RawHeader(h)
		}
		if name == "" {
			name = "col" + strconv.Itoa(i+1)
		}
		if _, dup := seen[asciiFold(name)]; dup {
			base := name
			for n := 2; ; n++ {
				candidate := base + "_" + strconv.Itoa(n)
				if _, dup := seen[asciiFold(candidate)]; !dup {
					name = candidate
					break
				}
			}
		}
		out[i] = name
		seen[asciiFold(name)] = struct{}{}
	}
	return out
}

// SanitizeTableName 把任意文本转成安全的表名：
//   - 非法字符（空格、短横线、点号等）替换为下划线；
//   - 去掉首尾下划线；
//   - 以数字开头时前缀 sheet_，避免生成非法标识符；
//   - 空字符串兜底为 data。
func SanitizeTableName(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Map(func(r rune) rune {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r), r == '_':
			return r
		}
		return '_'
	}, s)
	s = strings.Trim(s, "_")
	if s == "" {
		return "data"
	}
	if r := []rune(s)[0]; unicode.IsDigit(r) {
		s = "sheet_" + s
	}
	if !isBareIdent(s) {
		return "data"
	}
	return s
}

// isBareIdent 判断是否是合法的裸标识符（不以数字开头、只含字母数字下划线）。
func isBareIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if r == '_' || unicode.IsLetter(r) {
			continue
		}
		if i > 0 && unicode.IsDigit(r) {
			continue
		}
		return false
	}
	return true
}

// TableNameFromPath 由文件路径推导表名（去目录、去扩展名并做安全化处理）。
// CSV/TSV 没有 Sheet 名，默认用它作为表名。
func TableNameFromPath(path string) string {
	base := filepath.Base(path)
	return SanitizeTableName(strings.TrimSuffix(base, filepath.Ext(base)))
}

// autoHeader 生成 col1..colN 形式的列名（无表头模式使用）。
func autoHeader(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = "col" + strconv.Itoa(i+1)
	}
	return out
}

// isBlankRow 判断整行是否为空（Excel/CSV 常见的空行）。
func isBlankRow(row []string) bool {
	for _, v := range row {
		if strings.TrimSpace(v) != "" {
			return false
		}
	}
	return true
}
