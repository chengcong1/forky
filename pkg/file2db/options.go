package file2db

// 文本编码常量，供 CSV 数据源使用。
const (
	EncodingAuto    = "auto" // 自动探测（BOM / UTF-8 校验失败则按 GB18030 处理）
	EncodingUTF8    = "utf-8"
	EncodingGBK     = "gbk"
	EncodingGB18030 = "gb18030"
	EncodingUTF16LE = "utf-16le"
	EncodingUTF16BE = "utf-16be"
)

// readConfig 汇集各种数据源共用的读取选项。
type readConfig struct {
	HeaderRow     int  // 第几个非空行作为表头，从 1 开始，默认 1
	SkipRows      int  // 表头之后继续跳过多少行
	NoHeader      bool // 无表头模式，列名自动生成 col1..colN
	KeepRawHeader bool // 保留原始表头（只去空白 / BOM，不做小写化与标点替换）

	Sheets          []string // Excel：Sheet 名，留空使用活动 Sheet
	SkipSheets      []string // Excel：跳过指定 Sheet
	RawCellValue    bool     // Excel：使用单元格原始值
	OnlyActiveSheet bool     // Excel：只处理活动 Sheet

	Delimiter        rune   // CSV：字段分隔符，默认 ','
	Encoding         string // CSV：文本编码，默认 auto
	LazyQuotes       bool   // CSV：容忍不规范的引号
	Comment          rune   // CSV：注释字符
	TrimLeadingSpace bool   // CSV：去掉字段前导空白
	ReuseRecord      bool   // CSV：复用返回切片（省内存，但转瞬即失效，默认 false）
	FieldsPerRecord  int    // CSV：每行列数限制，0 表示按表头，-1 表示不限制
}

func defaultReadConfig() readConfig {
	return readConfig{
		HeaderRow:       1,
		Delimiter:       ',', // 不能留 0，否则 encoding/csv 会报 invalid delimiter
		Encoding:        EncodingAuto,
		LazyQuotes:      true,
		FieldsPerRecord: -1,
		OnlyActiveSheet: false,
	}
}

// Option 是所有数据源共用的配置项。
type Option func(*readConfig)

// ExcelOption、CSVOption 为 Option 的别名，仅用于提升可读性。
type (
	ExcelOption = Option
	CSVOption   = Option
)

// WithHeaderRow 指定表头所在行（按非空行计数，从 1 开始，默认 1）。
func WithHeaderRow(n int) Option {
	return func(c *readConfig) {
		if n > 0 {
			c.HeaderRow = n
		}
	}
}

// WithSkipRows 在表头之后继续跳过 n 行（小计行、说明行等）。
func WithSkipRows(n int) Option {
	return func(c *readConfig) {
		if n > 0 {
			c.SkipRows = n
		}
	}
}

// WithNoHeader 声明数据源没有表头，列名自动生成 col1..colN。
func WithNoHeader() Option {
	return func(c *readConfig) { c.NoHeader = true }
}

// WithRawHeader 保留数据源里的原始列名：只去掉首尾空白与 BOM，
// 不改大小写、也不把空格 / 括号 / 连字符替换成下划线。
// 例如 "创建时间 (UTC)"、"name" 会原样作为列名（查询时用双引号包裹即可）。
// 空列名仍补 colN，重名列仍加 _2 后缀，与自增主键 id 同名的列会被改名。
func WithRawHeader() Option {
	return func(c *readConfig) { c.KeepRawHeader = true }
}

// WithSheet 指定 Excel 中要读取的单个 Sheet 名（留空则使用活动 Sheet）。
// 等价于 WithSheets([]string{name})。
func WithSheet(name string) Option {
	return func(c *readConfig) {
		if name != "" {
			c.Sheets = []string{name}
		}
	}
}

// WithSheets 指定 Excel 中要读取的多个 Sheet 名，留空使用活动 Sheet。
func WithSheets(names []string) Option {
	return func(c *readConfig) { c.Sheets = names }
}

// WithSkipSheets 跳过指定 Excel 的 Sheet 名。
func WithSkipSheets(names []string) Option {
	return func(c *readConfig) { c.SkipSheets = names }
}

// WithOnlyActiveSheet 仅处理活动 Sheet。
func WithOnlyActiveSheet(onlyActiveSheet bool) Option {
	return func(c *readConfig) { c.OnlyActiveSheet = onlyActiveSheet }
}

// WithRawCellValue 让 Excel 返回单元格原始值（不做日期等格式化转换）。
func WithRawCellValue(raw bool) Option {
	return func(c *readConfig) { c.RawCellValue = raw }
}

// WithDelimiter 指定 CSV 字段分隔符（默认 ','；TSV 传 '\t'）。
func WithDelimiter(r rune) Option {
	return func(c *readConfig) {
		if r != 0 {
			c.Delimiter = r
		}
	}
}

// WithEncoding 指定 CSV 文本编码：auto（默认）、utf-8、gbk、gb18030、utf-16le、utf-16be。
func WithEncoding(name string) Option {
	return func(c *readConfig) {
		if name != "" {
			c.Encoding = name
		}
	}
}

// WithLazyQuotes 容忍不规范的引号（默认 true，便于处理网管导出的脏数据）。
func WithLazyQuotes(b bool) Option {
	return func(c *readConfig) { c.LazyQuotes = b }
}

// WithComment 指定注释字符，以其开头的行会被忽略。
func WithComment(r rune) Option {
	return func(c *readConfig) { c.Comment = r }
}

// WithTrimLeadingSpace 去掉字段的前导空白（默认 false，保留原值更安全）。
func WithTrimLeadingSpace(b bool) Option {
	return func(c *readConfig) { c.TrimLeadingSpace = b }
}

// WithFieldsPerRecord 设置每行字段数限制：0 按表头列数、-1 不限制（默认 -1）。
func WithFieldsPerRecord(n int) Option {
	return func(c *readConfig) { c.FieldsPerRecord = n }
}
