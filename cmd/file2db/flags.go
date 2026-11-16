package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
)

// version 是 -version 打印的版本号。
const version = "0.1.0"

// stringList 是「可以重复出现」的字符串参数，例如 -file a.xlsx -file b.csv。
type stringList []string

// String 实现 flag.Value；顺带用于帮助信息里的默认值显示。
func (s *stringList) String() string { return strings.Join(*s, ",") }

// Set 追加一个取值；空串直接忽略，方便在脚本里传空变量。
func (s *stringList) Set(v string) error {
	if v = strings.TrimSpace(v); v != "" {
		*s = append(*s, v)
	}
	return nil
}

// cliFlags 收集「只能从命令行给」的取值；能在配置文件里写的字段都放在 Config 上。
type cliFlags struct {
	config       string // -config
	configFormat string // -config-format

	// 导入：数据源与写入
	files      stringList // -file
	sheets     stringList // -sheet
	skipSheets stringList // -skip-sheet
	setup      stringList // -setup
	colTypes   stringList // -column-types
	indexes    stringList // -index
	uniqIdx    stringList // -unique-index
	tables     stringList // -tables

	// 查询：临时 SQL 与它的输出去向
	sql         string // -sql
	sqlFile     string // -sql-file
	sqlOut      string // -sql-out
	sqlSheet    string // -sql-sheet
	sqlNoHeader bool   // -sql-no-header
	sqlProvided bool   // 命令行是否真的给了 SQL（决定要不要跑配置文件里的 queries）

	// 输出设置：同时作用于 export、query 与 tables，所以先存下来再分发
	format       string // -format
	formatSet    bool   // -format 是否真的给了（用来区分「没给」与「与 export 相同」）
	outEncoding  string // -out-encoding
	outDelimiter string // -out-delimiter
	outBOM       bool   // -bom

	quiet   bool // -quiet
	version bool // -version
}

// invocation 是一次调用的解析结果。
type invocation struct {
	cmd string        // import / export / query / tables / version / help，空表示按配置文件依次执行
	cfg *Config       // 已合并「默认值 + 配置文件 + 命令行」的配置
	fs  *flag.FlagSet // 打印帮助用
}

// parseArgs 解析命令行参数。
//
// 顺序是刻意的：先只挑出 -config 并加载配置文件，再注册其余参数，
// 注册时的默认值就是配置文件里的值。于是「不写 = 用配置文件的值，写了 = 覆盖」
// 自动成立，不需要再写一遍容易漏字段的合并代码。
func parseArgs(args []string) (*invocation, error) {
	cfg := defaultConfig()

	// 配置文件必须最先读：它的值就是接下来各参数注册时的默认值
	path := scanStringFlag(args, "config")
	format := scanStringFlag(args, "config-format")
	if path != "" {
		if err := LoadConfig(path, format, &cfg); err != nil {
			return nil, err
		}
	}
	cfg.cli.config, cfg.cli.configFormat = path, format
	cfg.normalize()

	fv := &cfg.cli
	fs := flag.NewFlagSet("file2db-cli", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // 错误与 -h 的输出由我们自己打印

	// ---------- 数据库 ----------
	fs.StringVar(&cfg.DB.Driver, "driver", cfg.DB.Driver, "数据库驱动：sqlite（默认）/ mysql / postgres")
	fs.StringVar(&cfg.DB.DSN, "dsn", cfg.DB.DSN, "数据源：SQLite 是库文件路径或 :memory:；MySQL/Postgres 用各自 DSN")
	fs.IntVar(&cfg.DB.MaxOpenConns, "max-open-conns", cfg.DB.MaxOpenConns, "连接池上限；文件库保持 1 最稳")
	fs.Var(&fv.setup, "setup", "建连后额外执行的语句，可重复")

	// ---------- 导入：要读哪些文件、怎么读 ----------
	fs.Var(&fv.files, "file", "要导入的文件，可重复（也可以直接写在命令末尾）")
	fs.Var(&fv.sheets, "sheet", "Excel：只导入这些 Sheet，可重复，也可逗号分隔")
	fs.Var(&fv.skipSheets, "skip-sheet", "Excel：跳过这些 Sheet，可重复，也可逗号分隔")
	fs.BoolVar(&cfg.Import.OnlyActiveSheet, "only-active-sheet", cfg.Import.OnlyActiveSheet, "Excel：只处理活动 Sheet")
	fs.BoolVar(&cfg.Import.RawCellValue, "raw-cell-value", cfg.Import.RawCellValue, "Excel：使用单元格原始值")
	fs.IntVar(&cfg.Import.HeaderRow, "header-row", cfg.Import.HeaderRow, "表头所在行（按非空行计数），默认 1")
	fs.IntVar(&cfg.Import.SkipRows, "skip-rows", cfg.Import.SkipRows, "表头之后再跳过几行")
	fs.BoolVar(&cfg.Import.NoHeader, "no-header", cfg.Import.NoHeader, "数据源没有表头，列名自动生成 col1..colN")
	fs.BoolVar(&cfg.Import.RawHeader, "raw-header", cfg.Import.RawHeader, "保留原始表头（不做小写化与下划线替换）")
	fs.StringVar(&cfg.Import.Encoding, "encoding", cfg.Import.Encoding, "CSV 编码：auto/utf-8/gbk/gb18030/utf-16le/utf-16be")
	fs.StringVar(&cfg.Import.Delimiter, "delimiter", cfg.Import.Delimiter, `CSV 分隔符，默认 ","；制表符写 \t 或 tab`)
	fs.BoolVar(&cfg.Import.LazyQuotes, "lazy-quotes", cfg.Import.LazyQuotes, "CSV：容忍不规范的引号，默认 true")
	fs.StringVar(&cfg.Import.Comment, "comment", cfg.Import.Comment, "CSV：注释字符，以其开头的行会被忽略")
	fs.BoolVar(&cfg.Import.TrimLeadingSpace, "trim-leading-space", cfg.Import.TrimLeadingSpace, "CSV：去掉字段前导空白")
	fs.IntVar(&cfg.Import.FieldsPerRecord, "fields-per-record", cfg.Import.FieldsPerRecord, "CSV：每行列数限制，0 按表头，-1 不限制")

	// ---------- 导入：如何写库 ----------
	fs.StringVar(&cfg.Import.Table, "table", cfg.Import.Table, "目标表名；只有单表数据源（CSV / 单个 Sheet）时生效")
	fs.BoolVar(&cfg.Import.CreateTable, "create-table", cfg.Import.CreateTable, "自动建表，默认 true")
	fs.BoolVar(&cfg.Import.Replace, "replace", cfg.Import.Replace, "导入前 DROP TABLE，即全量覆盖")
	fs.IntVar(&cfg.Import.BatchSize, "batch", cfg.Import.BatchSize, "每条事务写入的行数，默认 500")
	fs.StringVar(&cfg.Import.DefaultType, "default-type", cfg.Import.DefaultType, "未指定列的默认类型，默认 TEXT")
	fs.Var(&fv.colTypes, "column-types", "指定列类型，格式 列名=类型，可逗号分隔或重复，如 name=INTEGER,age=INTEGER")
	fs.Var(&fv.indexes, "index", "导入完成后建的索引列，可重复，如 -index name -index name,age")
	fs.Var(&fv.uniqIdx, "unique-index", "导入完成后建的唯一索引列，可重复")
	fs.BoolVar(&cfg.Import.KeepBlankRows, "keep-blank-rows", cfg.Import.KeepBlankRows, "保留全空行，默认跳过")
	fs.Int64Var(&cfg.Import.Progress, "progress", cfg.Import.Progress, "每导入多少行打一次进度，0 关闭")

	// ---------- 导出 ----------
	fs.StringVar(&cfg.Export.Dir, "out", cfg.Export.Dir, "导出目录，一表一个文件")
	fs.Var(&fv.tables, "tables", "只导出这些表，可重复或逗号分隔；留空导出全部用户表")
	fs.StringVar(&cfg.Export.SingleFile, "single-file", cfg.Export.SingleFile, "多张表写进同一个 xlsx（每表一个 Sheet）")
	fs.BoolVar(&cfg.Export.Index, "export-index", cfg.Export.Index, "是否导出自增主键 id 列，默认 true")
	fs.Int64Var(&cfg.Export.Progress, "out-progress", cfg.Export.Progress, "每导出多少行打一次进度，0 关闭")

	// ---------- 查询 ----------
	fs.StringVar(&fv.sql, "sql", "", "要执行的 SQL；给定时只跑这一条，配置文件里的 queries 忽略")
	fs.StringVar(&fv.sqlFile, "sql-file", "", "从文件读取 SQL（相对路径按当前目录）")
	fs.StringVar(&fv.sqlOut, "sql-out", "", "查询结果输出文件（.csv / .xlsx）；留空打印到控制台")
	fs.StringVar(&fv.sqlSheet, "sql-sheet", "", "查询结果写入 xlsx 时的 Sheet 名，默认 Sheet1")
	fs.BoolVar(&fv.sqlNoHeader, "sql-no-header", false, "查询结果不写表头行")

	// ---------- 输出设置（export / query / tables 共用）----------
	fv.format, fv.outEncoding, fv.outDelimiter, fv.outBOM =
		cfg.Export.Format, cfg.Export.Encoding, cfg.Export.Delimiter, cfg.Export.BOM
	fs.StringVar(&fv.format, "format", fv.format, "输出格式：csv / xlsx（query 还支持 table，即控制台对齐打印）")
	fs.StringVar(&fv.outEncoding, "out-encoding", fv.outEncoding, "输出 CSV 编码：utf-8（默认）/ gbk / gb18030 / utf-16le / utf-16be")
	fs.StringVar(&fv.outDelimiter, "out-delimiter", fv.outDelimiter, `输出 CSV 分隔符，默认 ","`)
	fs.BoolVar(&fv.outBOM, "bom", fv.outBOM, "输出 CSV 写 BOM（Excel 打开 UTF-8 中文才不乱码）")

	// ---------- 其它 ----------
	fs.StringVar(&fv.config, "config", path, "配置文件路径（.json / .yaml / .toml）；不给命令时按配置顺序跑 import -> queries -> export")
	fs.StringVar(&fv.configFormat, "config-format", format, "配置文件格式：json / yaml / toml；默认按扩展名判断")
	fs.BoolVar(&fv.quiet, "quiet", cfg.Quiet, "不再输出导入 / 导出的进度日志")
	fs.StringVar(&cfg.Timeout, "timeout", cfg.Timeout, "整次执行的总超时，如 30m / 2h；0 或留空表示不超时")
	fs.BoolVar(&fv.version, "version", false, "打印版本信息后退出")

	// 允许位置参数与参数混排：flag 包解析到第一个非 flag 就停下，这里把剩下的喂回去继续解析。
	rest := args
	cmd := ""
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		cmd, rest = rest[0], rest[1:]
	}
	var positional []string
	for {
		if err := fs.Parse(rest); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return &invocation{cmd: "help", cfg: &cfg, fs: fs}, nil
			}
			return nil, err
		}
		rest = fs.Args()
		if len(rest) == 0 {
			break
		}
		positional = append(positional, rest[0])
		rest = rest[1:]
	}

	if fv.version {
		return &invocation{cmd: "version", cfg: &cfg, fs: fs}, nil
	}

	// ---------- 只有显式给出的参数才覆盖配置 ----------
	visited := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { visited[f.Name] = true })
	set := func(name string) bool { return visited[name] }

	if set("quiet") {
		cfg.Quiet = fv.quiet
	}
	if set("setup") {
		cfg.DB.Setup = append(cfg.DB.Setup, fv.setup...)
	}
	if set("file") {
		cfg.Import.Files = append(cfg.Import.Files, fv.files...)
	}
	if set("sheet") {
		cfg.Import.Sheets = append(cfg.Import.Sheets, flatten(fv.sheets)...)
	}
	if set("skip-sheet") {
		cfg.Import.SkipSheets = append(cfg.Import.SkipSheets, flatten(fv.skipSheets)...)
	}
	if set("column-types") {
		types, err := parseColumnTypes(fv.colTypes)
		if err != nil {
			return nil, err
		}
		if cfg.Import.ColumnTypes == nil {
			cfg.Import.ColumnTypes = make(map[string]string, len(types))
		}
		for k, v := range types {
			cfg.Import.ColumnTypes[k] = v
		}
	}
	if set("index") {
		cfg.Import.Indexes = append(cfg.Import.Indexes, parseIndexes(fv.indexes, false)...)
	}
	if set("unique-index") {
		cfg.Import.Indexes = append(cfg.Import.Indexes, parseIndexes(fv.uniqIdx, true)...)
	}
	if set("tables") {
		cfg.Export.Tables = append(cfg.Export.Tables, flatten(fv.tables)...)
	}
	if set("format") {
		fv.formatSet = true
		cfg.Export.Format = fv.format
		for i := range cfg.Queries {
			cfg.Queries[i].Format = fv.format
		}
	}
	if set("out-encoding") {
		cfg.Export.Encoding = fv.outEncoding
		for i := range cfg.Queries {
			cfg.Queries[i].Encoding = fv.outEncoding
		}
	}
	if set("out-delimiter") {
		cfg.Export.Delimiter = fv.outDelimiter
		for i := range cfg.Queries {
			cfg.Queries[i].Delimiter = fv.outDelimiter
		}
	}
	if set("bom") {
		cfg.Export.BOM = fv.outBOM
		for i := range cfg.Queries {
			cfg.Queries[i].BOM = fv.outBOM
		}
	}

	// ---------- 位置参数按命令归类 ----------
	switch cmd {
	case "":
	case "import", "in", "i":
		cmd = "import"
		cfg.Import.Files = append(cfg.Import.Files, positional...)
	case "export", "out", "e":
		cmd = "export"
		cfg.Export.Tables = append(cfg.Export.Tables, flatten(stringList(positional))...)
	case "query", "sql", "q":
		cmd = "query"
		if len(positional) > 0 {
			fv.sql = strings.Join(positional, " ")
		}
	case "tables", "ls":
		cmd = "tables"
	case "help":
	case "version":
	default:
		return nil, fmt.Errorf("未知命令 %q；可用命令：import / export / query / tables / version / help", cmd)
	}
	fv.sqlProvided = fv.sql != "" || fv.sqlFile != ""

	return &invocation{cmd: cmd, cfg: &cfg, fs: fs}, nil
}

// flatten 把「可重复 + 可逗号分隔」的取值摊平：-sheet a,b -sheet c -> [a b c]。
func flatten(items stringList) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, splitList(item)...)
	}
	return out
}

// scanStringFlag 从原始参数里挑出某个字符串参数的取值（-name x 与 -name=x 都认，取最后一次）。
// 配置文件名及格式必须这样先扫出来：它们决定了其余参数注册时的默认值。
func scanStringFlag(args []string, name string) string {
	value := ""
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-"+name || a == "--"+name:
			if i+1 < len(args) {
				value = args[i+1]
				i++
			}
		case strings.HasPrefix(a, "-"+name+"="), strings.HasPrefix(a, "--"+name+"="):
			value = a[strings.Index(a, "=")+1:]
		}
	}
	return strings.TrimSpace(value)
}

// printUsage 打印用法、示例与全部参数。
func printUsage(w io.Writer, fs *flag.FlagSet) {
	fmt.Fprint(w, `file2db-cli —— file2db 的命令行版：Excel / CSV <-> 数据库（默认 SQLite）

用法：
  file2db-cli <命令> [参数...]
  file2db-cli -config 任务.yaml      # 不给命令时，按配置文件的顺序执行：import -> queries -> export

配置文件支持 JSON / YAML / TOML，按扩展名识别（认不出来时用 -config-format 指定），
三种格式的字段名完全一样；YAML / TOML 可以写注释，参数多的时候更直观。
字段说明见 cmd/cmd/README.md，模板见 example.yaml / example.toml / example.json，如不需要queries和export的过程在配置文件中删除相关配置即可。

命令：
  import    文件 -> 数据库（.xlsx/.xlsm/.csv/.tsv/.txt，按扩展名自动识别）
  export    数据库 -> CSV / XLSX（一表一文件，或 -single-file 合成一个 xlsx）
  query     执行 SQL，结果写 CSV / XLSX 或对齐打印到控制台
  tables    列出库里的表与行数
  version   打印版本
  help      打印本帮助（-h 同效）

参数的三层覆盖关系（低 -> 高）：
  内置默认值  <  -config 指定的 JSON 配置文件  <  命令行参数
所以「配置文件放常用配置，命令行只改临时那几项」是最省事的用法；
列表类参数（-file/-sheet/-index/-tables）是追加，不会清掉配置文件里的值。

示例：
  # 1) 参数全写在命令行：文件数据导进 SQLite
  file2db-cli import -dsn data.db -replace -raw-header -skip-sheet Index -skip-sheet HideEnum -skip-rows 4 -index colname -file data.xlsx

  # 2) 用一个配置文件描述整套流程（导入 -> 查询 -> 导出）
  file2db-cli -config etl.json

  # 3) 看看某张表的样子（控制台对齐打印，最多几十行时才建议这么看）
  file2db-cli query -dsn data.db -sql "SELECT * FROM users LIMIT 30"

  # 4) 查询结果直接落成 xlsx（Sheet 名自定义）
  file2db-cli query -dsn data.db -sql "SELECT * FROM users" -sql-out users.xlsx -sql-sheet users

  # 5) 把库里所有表导出成一个 xlsx，每表一个 Sheet（不要主键列）
  file2db-cli export -dsn data.db -out out -format xlsx -single-file 汇总.xlsx -export-index=false

  # 6) 换库只改驱动与 DSN
  file2db-cli export -driver mysql -dsn "user:pass@tcp(127.0.0.1:3306)/db?charset=utf8mb4" -out out

参数（默认值即当前生效的配置值，写出来就覆盖）：
`)
	if fs != nil {
		// 注册 FlagSet 时把输出丢进了 io.Discard（不想让 flag 包自己打帮助），
		// 打印默认值前得先接回这里。
		fs.SetOutput(w)
		fs.PrintDefaults()
	}
}
