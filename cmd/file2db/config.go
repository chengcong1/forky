package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/chengcong1/forky/pkg/file2db"

	"github.com/BurntSushi/toml"
	"gopkg.in/yaml.v3"
)

// defaultDSN 是 SQLite 的默认库文件。
const defaultDSN = "data.db"

// 配置文件格式；由 -config 的扩展名或 -config-format 决定。
const (
	FormatJSON = "json"
	FormatYAML = "yaml"
	FormatTOML = "toml"
)

// Config 是命令行工具的全部配置。它的取值有三个来源，优先级从低到高：
//
//  1. defaultConfig() 里的内置默认值；
//  2. -config 指定的 JSON 配置文件（只覆盖文件里出现过的字段）；
//  3. 命令行参数（显式写出来才覆盖）。
//
// 所以最省事的用法是「一个配置文件 + 少量临时参数」：参数再多也不用每次敲一遍。
type Config struct {
	DB      DBConfig      `json:"db"`
	Import  ImportConfig  `json:"import"`
	Export  ExportConfig  `json:"export"`
	Queries []QueryConfig `json:"queries"`

	Quiet bool `json:"quiet"` // 关掉导入 / 导出的进度日志；工具自己的汇总行仍会打印

	// Timeout 是整次执行的总超时，写法同 time.ParseDuration（"30m"、"2h"）；
	// 空或 "0" 表示不超时。适合放进定时任务的配置里，避免任务卡死占住调度线程。
	Timeout string `json:"timeout"`

	cli cliFlags // 只由命令行填充，不参与 JSON（见 flags.go）
}

// DBConfig 数据库连接配置。
type DBConfig struct {
	Driver       string   `json:"driver"`       // sqlite（默认）/ mysql / postgres
	DSN          string   `json:"dsn"`          // SQLite 是库文件路径；其它库写各自的 DSN
	MaxOpenConns int      `json:"maxOpenConns"` // 连接池上限，默认 1
	Setup        []string `json:"setup"`        // 建连之后额外执行的语句
}

// ImportConfig 描述「文件 -> 数据库」这一步。
type ImportConfig struct {
	Files []string `json:"files"` // 要导入的文件：.xlsx/.xlsm/.csv/.tsv/.txt

	Table         string            `json:"table"`         // 目标表名；只有「单表数据源」时生效
	CreateTable   bool              `json:"createTable"`   // 自动建表，默认 true
	Replace       bool              `json:"replace"`       // 导入前 DROP TABLE，默认 false
	BatchSize     int               `json:"batchSize"`     // 每条事务的行数，默认 500
	DefaultType   string            `json:"defaultType"`   // 未指定列的默认类型，默认 TEXT
	ColumnTypes   map[string]string `json:"columnTypes"`   // 指定列类型，如 {"age":"INTEGER"}
	Indexes       []IndexConfig     `json:"indexes"`       // 导入完成后建的索引
	KeepBlankRows bool              `json:"keepBlankRows"` // 保留全空行，默认 false
	Progress      int64             `json:"progress"`      // 每多少行打一次进度，0 关闭

	// 以下是读取选项，Excel 与 CSV 各取所需
	Sheets           []string `json:"sheets"`           // Excel：只导入这些 Sheet，留空取活动 Sheet
	SkipSheets       []string `json:"skipSheets"`       // Excel：跳过这些 Sheet
	OnlyActiveSheet  bool     `json:"onlyActiveSheet"`  // Excel：只处理活动 Sheet
	RawCellValue     bool     `json:"rawCellValue"`     // Excel：使用单元格原始值
	HeaderRow        int      `json:"headerRow"`        // 表头所在行（按非空行计数），默认 1
	SkipRows         int      `json:"skipRows"`         // 表头之后再跳过几行，默认 0
	NoHeader         bool     `json:"noHeader"`         // 无表头，列名自动生成 col1..colN
	RawHeader        bool     `json:"rawHeader"`        // 保留原始表头（不清洗成小写下划线）
	Encoding         string   `json:"encoding"`         // CSV 编码：auto/utf-8/gbk/gb18030/utf-16le/utf-16be
	Delimiter        string   `json:"delimiter"`        // CSV 分隔符，如 ","、"\t"
	LazyQuotes       bool     `json:"lazyQuotes"`       // CSV：容忍不规范的引号，默认 true
	Comment          string   `json:"comment"`          // CSV：注释字符，以其开头的行会被忽略
	TrimLeadingSpace bool     `json:"trimLeadingSpace"` // CSV：去掉字段前导空白
	FieldsPerRecord  int      `json:"fieldsPerRecord"`  // CSV：每行列数限制，0 按表头，-1 不限制
}

// ExportConfig 描述「数据库 -> 文件」这一步。
type ExportConfig struct {
	Dir        string   `json:"dir"`        // 输出目录；一表一个文件
	Tables     []string `json:"tables"`     // 只导出这些表，留空导出全部用户表
	Format     string   `json:"format"`     // csv / xlsx；留空：目录模式按 csv，单文件按后缀
	Index      bool     `json:"index"`      // 是否导出自增主键 id 列，默认 true
	SingleFile string   `json:"singleFile"` // 非空时多张表写进同一个 xlsx（每表一个 Sheet）
	Delimiter  string   `json:"delimiter"`  // CSV 分隔符，默认 ","
	Encoding   string   `json:"encoding"`   // CSV 输出编码，默认 utf-8
	BOM        bool     `json:"bom"`        // CSV 是否写 BOM（Excel 打开 UTF-8 中文才不乱码）
	Progress   int64    `json:"progress"`   // 每多少行打一次进度，0 关闭
}

// QueryConfig 描述一条 SQL 及其输出去向。
type QueryConfig struct {
	Name      string `json:"name"`      // 只用于日志，可留空
	SQL       string `json:"sql"`       // 内联 SQL
	SQLFile   string `json:"sqlFile"`   // 从文件读 SQL（与 sql 二选一；相对路径按配置文件所在目录解析）
	Out       string `json:"out"`       // 输出文件：.csv / .xlsx；留空打印到控制台
	Format    string `json:"format"`    // csv / xlsx / table；留空按 Out 后缀，控制台默认 table
	Sheet     string `json:"sheet"`     // xlsx 的 Sheet 名，留空用 Sheet1
	Delimiter string `json:"delimiter"` // CSV 分隔符，留空继承 export.delimiter
	Encoding  string `json:"encoding"`  // 输出编码，留空继承 export.encoding
	BOM       bool   `json:"bom"`       // CSV 是否写 BOM
	NoHeader  bool   `json:"noHeader"`  // 不写表头行
}

// IndexConfig 描述一个索引，JSON 里可以写三种写法：
//
//	"indexes": ["Name"]                               // 单列
//	"indexes": ["Name,Age"]                // 逗号分隔的复合索引
//	"indexes": [{"name":"idx_age","columns":["age"],"unique":true}]
//
// 列名写原始表头（"age"）或清洗后的列名（"age"）都能对上；
// 多 Sheet 导入时，哪张表没这列就跳过哪张表的索引。
type IndexConfig struct {
	Name    string   `json:"name"`    // 索引名，留空由 file2db 自动生成
	Columns []string `json:"columns"` // 索引列（按顺序）
	Unique  bool     `json:"unique"`  // 唯一索引；数据有重复值时导入会报错
}

// indexConfigAlias 用于在自定义 UnmarshalJSON 里绕开递归。
type indexConfigAlias IndexConfig

// UnmarshalJSON 让同一个字段既能写字符串、字符串数组，也能写对象。
func (c *IndexConfig) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	switch {
	case trimmed == "" || trimmed == "null":
		return nil
	case trimmed[0] == '"': // "列1,列2"
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		*c = IndexConfig{Columns: splitList(s)}
		return nil
	case trimmed[0] == '[': // ["列1","列2"]
		var cols []string
		if err := json.Unmarshal(data, &cols); err != nil {
			return err
		}
		*c = IndexConfig{Columns: cols}
		return nil
	default: // {"name":...,"columns":[...]}
		var obj indexConfigAlias
		if err := json.Unmarshal(data, &obj); err != nil {
			return err
		}
		*c = IndexConfig(obj)
		return nil
	}
}

// defaultConfig 返回全部默认值：默认 SQLite + data.db，读取按「首行表头、自动编码」，
// 导出按 CSV + UTF-8。
func defaultConfig() Config {
	return Config{
		DB: DBConfig{Driver: file2db.DriverName, DSN: defaultDSN, MaxOpenConns: 1},
		Import: ImportConfig{
			CreateTable:     true,
			BatchSize:       500,
			DefaultType:     "TEXT",
			HeaderRow:       1,
			Encoding:        file2db.EncodingAuto,
			Delimiter:       ",",
			LazyQuotes:      true,
			FieldsPerRecord: -1,
		},
		Export: ExportConfig{
			Index:     true,
			Encoding:  file2db.EncodingUTF8,
			Delimiter: ",",
		},
		Timeout: "0", // 默认不超时；定时任务建议显式配上
	}
}

// LoadConfig 读取配置文件并合并进 cfg，支持 JSON / YAML / TOML 三种格式。
//
// 只覆盖文件里出现过的字段，没写的保持默认值，所以配置文件可以只写关心的那几项；
// 字段名写错会直接报错，避免「改了配置却不生效」这种最难查的问题。
//
// 三种格式的字段名完全一样（就是下面结构体上的 json 标签），只是 YAML / TOML
// 更直观、还能写注释；格式按扩展名识别，认不出来时用 -config-format 指定：
//
//	file2db-cli -config task.yaml
//	file2db-cli -config task.conf -config-format toml
//
// 实现上 YAML / TOML 会先解成通用 map 再转成等价的 JSON，复用同一条解析路径，
// 于是「字段名拼错会报错」「indexes 支持字符串 / 数组 / 对象三种写法」
// 这些行为在三种格式里完全一致，不用维护三套解析与三套校验代码。
func LoadConfig(path, formatOverride string, cfg *Config) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("读取配置文件失败: %w", err)
	}
	// 记事本「UTF-8 带 BOM」另存时会多出 BOM，先去掉再解析
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})

	format, err := configFormat(path, formatOverride)
	if err != nil {
		return err
	}
	if format != FormatJSON {
		if data, err = formatToJSON(data, format); err != nil {
			return fmt.Errorf("解析 %s 配置文件 %s 失败: %w", strings.ToUpper(format), path, err)
		}
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(cfg); err != nil {
		return fmt.Errorf("解析 %s 配置文件 %s 失败: %s", strings.ToUpper(format), path, cleanJSONError(err))
	}
	return nil
}

// cleanJSONError 去掉 encoding/json 给错误加的 "json: " 前缀：
// 外层已经说清楚是哪种格式了，YAML / TOML 用户看到 json 字样容易一头雾水。
func cleanJSONError(err error) string {
	return strings.TrimPrefix(err.Error(), "json: ")
}

// configFormat 决定用哪个解析器：-config-format 显式指定优先，否则按扩展名。
func configFormat(path, override string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(override)) {
	case "":
	case FormatJSON:
		return FormatJSON, nil
	case FormatYAML, "yml":
		return FormatYAML, nil
	case FormatTOML, "tml":
		return FormatTOML, nil
	default:
		return "", fmt.Errorf("不支持的配置文件格式 %q（可选 json / yaml / toml）", override)
	}

	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		return FormatJSON, nil
	case ".yaml", ".yml":
		return FormatYAML, nil
	case ".toml", ".tml":
		return FormatTOML, nil
	}
	return "", fmt.Errorf("认不出配置文件 %s 的格式，请用 -config-format json|yaml|toml 指定", filepath.Base(path))
}

// formatToJSON 把 YAML / TOML 文本转成等价的 JSON。
// 空文件（或只有注释）按「全部使用默认值」处理，等价于空 JSON 对象。
func formatToJSON(data []byte, format string) ([]byte, error) {
	var raw any
	switch format {
	case FormatYAML:
		if err := yaml.Unmarshal(data, &raw); err != nil {
			return nil, err
		}
	case FormatTOML:
		// TOML 的键一定是字符串，直接解成 map[string]any
		var m map[string]any
		if err := toml.Unmarshal(data, &m); err != nil {
			return nil, err
		}
		raw = m
	}
	if raw == nil {
		return []byte("{}"), nil
	}
	js, err := json.Marshal(raw)
	if err != nil {
		// 一般出现在键不是字符串的时候（YAML 允许 1: xxx 这种写法）
		return nil, fmt.Errorf("配置内容无法转成通用结构（键必须是字符串，值只能是标量 / 数组 / 表）: %w", err)
	}
	return js, nil
}

// normalize 把「留空表示继承 / 自动」的字段补成具体值，免得下游到处判空。
func (c *Config) normalize() {
	if c.DB.Driver == "" {
		c.DB.Driver = file2db.DriverName
	}
	if c.DB.DSN == "" {
		c.DB.DSN = defaultDSN
	}
	if c.DB.MaxOpenConns <= 0 {
		c.DB.MaxOpenConns = 1
	}

	if c.Import.Encoding == "" {
		c.Import.Encoding = file2db.EncodingAuto
	}
	if c.Import.Delimiter == "" {
		c.Import.Delimiter = ","
	}
	if c.Import.HeaderRow <= 0 {
		c.Import.HeaderRow = 1
	}
	if c.Import.BatchSize <= 0 {
		c.Import.BatchSize = 500
	}
	if c.Import.DefaultType == "" {
		c.Import.DefaultType = "TEXT"
	}

	if c.Export.Encoding == "" {
		c.Export.Encoding = file2db.EncodingUTF8
	}
	if c.Export.Delimiter == "" {
		c.Export.Delimiter = ","
	}

	// 每条 query 的输出设置留空时继承 export 的编码 / 分隔符；
	// 但格式不继承：query 留空时按 out 后缀推断（.xlsx 就该出 Excel），
	// 写控制台则用对齐打印，避免被 export.format 带着走出奇怪的格式。
	for i := range c.Queries {
		q := &c.Queries[i]
		if q.Encoding == "" {
			q.Encoding = c.Export.Encoding
		}
		if q.Delimiter == "" {
			q.Delimiter = c.Export.Delimiter
		}
	}
}

// ---------- 小工具 ----------

// splitList 按逗号切分并丢掉空白项："a, b,," -> ["a" "b"]。
func splitList(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// parseDelimiter 把分隔符文本解析成 rune：支持 ","、";"、"|"、"\t"、tab、space。
func parseDelimiter(s string) (rune, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "":
		return 0, fmt.Errorf("分隔符不能为空")
	case `\t`, "tab", "t":
		return '\t', nil
	case "space":
		return ' ', nil
	}
	rs := []rune(strings.TrimSpace(s))
	if len(rs) != 1 {
		return 0, fmt.Errorf("分隔符只能是单个字符，收到 %q（制表符写 \\t 或 tab）", s)
	}
	return rs[0], nil
}

// parseColumnTypes 解析 "列名=类型" 形式（可重复、可用逗号分隔多个）。
func parseColumnTypes(items []string) (map[string]string, error) {
	out := make(map[string]string)
	for _, item := range items {
		for _, kv := range splitList(item) {
			name, typ, ok := strings.Cut(kv, "=")
			name, typ = strings.TrimSpace(name), strings.TrimSpace(typ)
			if !ok || name == "" || typ == "" {
				return nil, fmt.Errorf("列类型 %q 格式不对，应为 列名=类型，例如 age=INTEGER", kv)
			}
			out[name] = typ
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// parseIndexes 把 "列1,列2" 形式的索引参数转成 IndexConfig。
func parseIndexes(items []string, unique bool) []IndexConfig {
	out := make([]IndexConfig, 0, len(items))
	for _, item := range items {
		if cols := splitList(item); len(cols) > 0 {
			out = append(out, IndexConfig{Columns: cols, Unique: unique})
		}
	}
	return out
}
