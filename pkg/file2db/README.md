# file2db

**Excel / CSV ↔ 数据库** 的流式导入导出包。默认 SQLite（纯 Go，无需 CGO），也支持 MySQL / Postgres。

```go
ctx := context.Background()                                  // 超时 / Ctrl+C 都靠它

db, err := file2db.Imports(ctx,                               // 打开库 + 导入文件，一步到位
	[]string{"users.xlsx", "users.csv"},
	[]file2db.DBOption{file2db.WithDSN("data.db")},
	[]file2db.Option{file2db.WithRawHeader()},
	[]file2db.ImportOption{file2db.WithReplace(), file2db.WithIndex("name")},
)
if err != nil {
	log.Fatal(err)
}
defer db.Close()

// 之后就是一个普通的 *sql.DB：SQL 随便查
var n int
db.QueryRow(`SELECT COUNT(*) FROM users WHERE "name" LIKE '%张三%'`).Scan(&n)
```

## 特性

- **全程流式**：Excel 走 `excelize.Rows` 行迭代器、CSV 走 `encoding/csv`、导出走 `StreamWriter`，
  内存占用基本与文件大小无关；写入端预编译语句 + 每 `BatchSize` 行提交一次事务。
- **可超时 / 可中断**：导入与导出都收 `context.Context`。取消时导入在「一行 / 一批」的边界上停下，
  当前未提交的批次回滚、已提交的批次保留；导出会删掉半截产物。适合放进定时任务或 Web 请求里。
- **编码自动识别**：BOM → UTF-8 合法性校验 → 回退 GB18030（国内网管导出的 CSV 大多是 GBK）；
  导出支持 `gbk/gb18030/utf-16le/utf-16be` 与 BOM，方便 Excel 直接双击打开。
- **表头定位灵活**：第 N 个**非空行**当表头、表头后再跳几行、完全无表头（列名自动 `col1..colN`）。
- **列名清洗可控**：默认清洗（小写化、空格与标点变下划线）、重名自动加后缀、避让自增主键 `id`；
  加 `WithRawHeader()` 则原样保留表头文字。
- **导入完再建索引**：`WithIndex / WithUniqueIndex / WithIndexSpec`，列名写原始表头或清洗后的都能对上，
  多 Sheet 导入时哪张表没这列就跳过哪张表。
- **表名安全化**：非法字符替换、数字开头自动加前缀，不会生成非法标识符。
- **三方言**：SQLite / MySQL / Postgres，标识符引用、占位符（`?` / `$n`）、自增主键、元数据查询全部按方言生成；
  换库只改驱动与 DSN。
- **可复用外部连接**：`Wrap` 把已有的 `*sql.DB` / `*sqlx.DB` 包一层即可当目标库用。

## 代码组织

| 文件 | 内容 |
| --- | --- |
| `db.go` | `Open` / `Wrap` / `DB` / `Database` 接口 + `Tables`、`CountRows` 等元数据助手 |
| `dialect.go` | 方言接口与 SQLite / MySQL / Postgres 三套实现 |
| `options.go` | 读取选项（表头、Sheet、编码、分隔符…） |
| `import.go` | 写入选项与 `Result`、`IndexSpec` |
| `excel.go` / `csv.go` / `slice.go` | 三种数据源：`WorkBook` / `CSVSource` / `SliceSource` |
| `source.go` | 编排层：`Source` 接口、`NewSource(s)`、批量 `Import`、`Imports` |
| `writer.go` | 各数据源共用的写入端：表头定位、建表、批量事务、建索引 |
| `export.go` | 导出：`ExportTable` / `ExportTables` /（`slice.go` 里的）`ExportSlice` |
| `utils.go` | 列名 / 表名清洗等纯函数 |
| `*_test.go` | 单元测试（约 89% 覆盖率，见第六节） |

---

## 一、导入（文件 → 数据库）

### 1. 一层、两层还是三层？

| 用法 | 适用场景 |
| --- | --- |
| `file2db.Imports(ctx, paths, dbOpts, readOpts, impOpts)` | 打开库 + 导入一次搞定，返回可查询的 `*DB` |
| `file2db.Import(ctx, db, paths, readOpts, impOpts)` | 库已经开好了，只要逐文件的 `Result`（行数 / 耗时 / 索引） |
| `NewSource(path, opts...)` + `src.Import(ctx, db, impOpts...)` | 一个数据源要按不同参数导入多次，或要用 `WorkBook` / `CSVSource` / `SliceSource` 的专有能力 |

```go
// 第二层：拿每条结果
results, err := file2db.Import(ctx, db, []string{"a.xlsx", "b.csv"},
	[]file2db.Option{file2db.WithHeaderRow(1), file2db.WithRawHeader()},
	[]file2db.ImportOption{file2db.WithReplace(), file2db.WithLogger(log.Default())},
)
for _, r := range results {
	log.Printf("%v：%d 行（跳过 %d 行），索引 %v，耗时 %s", r.Tables, r.Rows, r.Skipped, r.Indexes, r.Elapsed)
}
```

```go
// 第三层：自己控制数据源生命周期
sources, err := file2db.NewSources([]string{"a.xlsx", "b.csv"}, file2db.WithRawHeader())
if err != nil {
	log.Fatal(err)
}
defer file2db.CloseAll(sources) // 无论成功与否都要关，避免文件句柄泄漏

for _, src := range sources {
	if _, err := src.Import(ctx, db, file2db.WithReplace()); err != nil {
		log.Fatal(err)
	}
}
```

### 2. Excel 数据源

```go
wb, err := file2db.NewExcel("users.xlsx",
	file2db.WithSheets([]string{"sheet1"}),   // 只读指定 Sheet
	file2db.WithSkipSheets([]string{"Index", "HideEnum"}), // 跳过辅助 Sheet
	file2db.WithHeaderRow(1),
	file2db.WithSkipRows(4),   // 表头后面还有 4 行说明
	file2db.WithRawHeader(),   // 保留原始表头
)
if err != nil {
	log.Fatal(err)
}
defer wb.Close()

res, err := wb.Import(ctx, db,
	file2db.WithTable("Sheet1"), // 只有「单个 Sheet」时生效
	file2db.WithReplace(),
	file2db.WithColumnTypes(map[string]string{"age": "INTEGER"}),
	file2db.WithIndex("name"),
)
```

Sheet 的选择规则（`NewExcel` 内部）：

| 配置 | 结果 |
| --- | --- |
| 都没给 | 全部 Sheet（`GetSheetList()` 的顺序） |
| `WithSkipSheets` | 全部 Sheet 减去被跳过的 |
| `WithSheets` | 只保留 `WithSheets ∩ 全部 Sheet`，再去掉被跳过的 |
| `WithSheets` + `WithSkipSheets` | 先取交集再减（两者确实**一起生效**，不会丢掉 `SkipSheets`） |
| `WithOnlyActiveSheet(true)` | 只处理活动 Sheet（只在没给 `WithSheets` 时生效） |
| 过滤后为空 | `NewExcel` 直接报错，并把工作簿里**实际可用的 Sheet 名**列出来（便于发现名字写错） |

**一个 Sheet 一张表**，表名默认取 Sheet 名。所以 `WithTable` 只在「过滤后正好剩一个 Sheet」时有效 ——
多张结构不同的表混进同一张表没有意义，多 Sheet 时它会被静默忽略。

### 3. CSV / TSV 数据源

```go
csvSrc, err := file2db.NewCSV("users.csv",
	file2db.WithEncoding(file2db.EncodingGBK), // auto（默认）/ utf-8 / gbk / gb18030 / utf-16le / utf-16be
	file2db.WithDelimiter('\t'),               // TSV 用制表符；NewTSV 已内置
	file2db.WithLazyQuotes(true),              // 容忍不规范的引号（默认 true）
	file2db.WithComment('#'),                  // 以 # 开头的行忽略
	file2db.WithTrimLeadingSpace(true),
)
if err != nil {
	log.Fatal(err)
}
defer csvSrc.Close()

log.Println("实际识别到的编码：", csvSrc.Encoding()) // 传 gbk/gb18030 时返回 gb18030（解码器是 GBK 超集）
res, err := csvSrc.Import(ctx, db, file2db.WithReplace())
```

编码识别顺序：BOM（UTF-8 / UTF-16LE / UTF-16BE）→ UTF-8 合法性校验 → 回退 GB18030。
`WithDelimiter` 留空时是 `','`；`.txt` 文件按 CSV 处理。

### 4. 内存切片数据源

```go
src := file2db.NewSlice([][]string{
	{"name", "age"},
	{"张三", "20"},
})
res, err := src.Import(ctx, db, file2db.WithReplace()) // 默认表名 data，可用 WithTable / WithDefaultTable 覆盖
src.Reset()                                       // 需要重复导入时把读取位置拨回开头
log.Println(src.Len())                            // 总行数（含表头）
```

### 5. 表名与列名怎么来的

**表名**（顺序：`WithTable` > 数据源自带 > `WithDefaultTable` > 兜底）：

| 数据源 | 默认表名 |
| --- | --- |
| Excel | Sheet 名 |
| CSV / TSV | 文件名去扩展名（`TableNameFromPath`） |
| 切片 | `data` |

所有表名都会过一遍 `SanitizeTableName`：

```
"2024-users (v2)"  ->  sheet_2024_users__v2     // 非法字符->下划线、首尾下划线去掉、数字开头加 sheet_
""                ->  data
```

**列名**默认过 `CleanHeaders(headers, false)`：

| 原始表头 | 清洗后 |
| --- | --- |
| `PCI` | `pci` |
| `创建时间 (UTC)` | `创建时间__utc` |
| `id` | `id_2`（避让导入时固定加的自增主键 `id`） |
| 空表头 | `colN`（按位置） |
| 重名列 | 第二个起加 `_2`、`_3`… |

`WithRawHeader()` 换成 `CleanHeaders(headers, true)`：只去首尾空白与 BOM，其余原样保留
（`PCI` 仍是 `PCI`、`创建时间 (UTC)` 保留括号；`id` 依旧会让位成 `id_2`）。
列名里有空格 / 括号时，查询用双引号包起来即可：`SELECT "创建时间 (UTC)" FROM users`。

### 6. 表头定位

| 选项 | 语义 |
| --- | --- |
| `WithHeaderRow(n)` | 第 n 个**非空行**作为表头（默认 1，表头之前的空行不计数） |
| `WithSkipRows(n)` | 表头之后再跳过 n 行（按物理行，用于小计行 / 说明行） |
| `WithNoHeader()` | 没有表头：第一行非空行就是数据，列名自动 `col1..colN` |

读不到任何非空行时，单个数据源会报「文件中没有可导入的数据」/「所有工作表都是空表」。

### 7. 列类型与写入行为

| 选项 | 默认 | 说明 |
| --- | --- | --- |
| `WithCreateTable(b)` | `true` | 自动建表（`CREATE TABLE IF NOT EXISTS`） |
| `WithReplace()` | 关 | 导入前 `DROP TABLE`，全量覆盖 |
| `WithDefaultColumnType(t)` | `TEXT` | 未指定列的默认类型 |
| `WithColumnTypes(map)` | 无 | 指定列类型，键写清洗后的列名，如 `{"pci": "INTEGER"}` |
| `WithBatchSize(n)` | `500` | 每条事务写入的行数，随后重开事务并重新预编译 |
| `WithKeepBlankRows()` | 关 | 保留全空行；默认跳过并计入 `Result.Skipped` |
| `WithLogger(l)` / `WithProgress(n)` | 无 / 0 | 每 n 行打一次进度（要同时给 Logger 才有输出） |

默认全列 `TEXT` 是刻意的：小区编号、`460-01` 这类值保留前导 0 与原始文本，最安全。
Excel 稀疏存储与 CSV 行尾缺列造成的「单元格数少于表头列数」会自动补齐空串。

### 8. 索引

```go
file2db.WithIndex("name"),                    // 单列
file2db.WithIndex("age", "sex"),              // 复合索引
file2db.WithUniqueIndex("name"),                     // 唯一索引：数据重复会直接报错
file2db.WithIndexSpec(file2db.IndexSpec{            // 自定义索引名
	Name: "idx_age", Columns: []string{"age"}, Unique: false,
}),
```

规则：

- 数据全部落库**之后**才建索引（一次性建比逐行维护快得多）；
- 列名写原始表头（`"AGE"`）或清洗后的列名（`"age"`）都能对上；对不上的列忽略；
- 某张表这次没写入任何数据（`Rows == 0`）就跳过该表的索引；
- 多 Sheet 导入时，哪张表没这些列就跳过哪张表的索引（不报错）；
- 不写名字时自动生成 `idx_<表名>_<列名...>`，超过 60 字节按字节截断并附 CRC32 后缀避免撞名；
- MySQL 不支持 `CREATE INDEX IF NOT EXISTS`，包内会先查 `information_schema` 再建。

### 9. 读取选项一览

`Option`（别名 `ExcelOption` / `CSVOption`）是同一组选项，创建数据源时按需作用到对应类型上，
所以 Excel 与 CSV 可以共用同一份读取选项：

| 选项 | 默认 | 作用 | 适用于 |
| --- | --- | --- | --- |
| `WithHeaderRow(n)` | `1` | 表头在第 n 个非空行 | 全部 |
| `WithSkipRows(n)` | `0` | 表头之后再跳 n 行 | 全部 |
| `WithNoHeader()` | 关 | 无表头，列名自动 `col1..colN` | 全部 |
| `WithRawHeader()` | 关 | 保留原始表头（不清洗） | 全部 |
| `WithSheet(name)` / `WithSheets(names)` | 空 | 只读指定 Sheet | Excel |
| `WithSkipSheets(names)` | 空 | 跳过指定 Sheet | Excel |
| `WithOnlyActiveSheet(b)` | `false` | 只处理活动 Sheet | Excel |
| `WithRawCellValue(b)` | `false` | 用单元格原始值（不做日期等格式化） | Excel |
| `WithDelimiter(r)` | `,` | 字段分隔符（TSV 用 `'\t'`） | CSV / TSV |
| `WithEncoding(name)` | `auto` | 文本编码 | CSV / TSV |
| `WithLazyQuotes(b)` | `true` | 容忍不规范的引号 | CSV / TSV |
| `WithComment(r)` | 无 | 注释字符，以其开头的行忽略 | CSV / TSV |
| `WithTrimLeadingSpace(b)` | `false` | 去掉字段前导空白 | CSV / TSV |
| `WithFieldsPerRecord(n)` | `-1` | 每行列数限制：`0` 按表头、`-1` 不限制 | CSV / TSV |

---

## 二、导出（数据库 → 文件）

```go
// 单表导一个文件：格式默认按输出后缀推断（.xlsx/.xlsm → Excel，其余 → CSV）
res, err := file2db.ExportTable(ctx, db, "Sheet", filepath.Join("out", "users.csv"),
	file2db.WithExportIndex(false),                    // 不导出 id 主键列
	file2db.WithExportEncoding(file2db.EncodingGBK),   // 国内 Excel 直接双击可看
	file2db.WithExportBOM(),                           // 或 utf-8 + BOM
	file2db.WithExportLogger(log.Default()),
	file2db.WithExportProgress(5000),
)
```

```go
// 多表导出到一个目录：默认一表一个文件；也可以合成一个 xlsx（每表一个 Sheet）
results, err := file2db.ExportTables(ctx, db, "out",
	file2db.WithExportFormat(file2db.FormatXLSX),
	file2db.WithExportTables("sheet1", "sheet2"),   // 留空 = 库中全部用户表
	file2db.WithExportSingleFile("data.xlsx"),
	file2db.WithExportIndex(false),
)
```

```go
// 产物留在内存里（第 0 行是列名）
rows, err := file2db.ExportSlice(ctx, db, "sheet1", file2db.WithExportIndex(false))
// rows[0] 是表头，rows[1:] 是数据
```

| 选项 | 默认 | 说明 |
| --- | --- | --- |
| `WithExportFormat(f)` | 按后缀推断 | `FormatCSV` / `FormatXLSX`；目录模式默认 CSV |
| `WithExportIndex(b)` | `true` | 是否导出自动加的 `id` 主键列 |
| `WithExportTables(names...)` | 全部用户表 | 只导出指定表（`ExportTables` 用） |
| `WithExportSingleFile(name)` | 无 | 多表写进同一个 xlsx；只支持 xlsx，相对路径挂在输出目录下 |
| `WithExportDelimiter(r)` | `,` | CSV 分隔符 |
| `WithExportEncoding(name)` | `utf-8` | CSV 编码：`utf-8/gbk/gb18030/utf-16le/utf-16be` |
| `WithExportBOM()` | 关 | 写 BOM（UTF-8 带 BOM 后 Excel 才能正确识别中文） |
| `WithExportLogger(l)` / `WithExportProgress(n)` | 无 / 0 | 进度日志 |

导出细节：

- 值是**统一按字符串**写出的：数字列不会因为 Excel 自动格式化而丢掉前导 0；
- 导出前会确认表存在并读一遍列清单，`WithExportIndex(false)` 时主键列**根本不会被查出来**（省 IO）；
- 单文件模式下 Sheet 名取表名，非法字符会替换、重名自动加 `_2`，首张表复用自带的 `Sheet1`；
- 中途失败会删掉半截产物（`os.Remove`），不会留下坏文件；
- 数据库里的 `NULL` 统一写成空串（不会出现 `converting NULL to string is unsupported`）。

---

## 二半、超时与中断

导入 / 导出的每个公开函数都收 `context.Context`，取消后能「停在安全的位置」：

```go
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
defer cancel()

res, err := file2db.Import(ctx, db, []string{"huge.csv"},
	[]file2db.Option{file2db.WithHeaderRow(1)},
	[]file2db.ImportOption{file2db.WithBatchSize(500)},
)
switch {
case errors.Is(err, context.DeadlineExceeded):
	log.Println("超时了：已提交的批次保留，当前批次已回滚")
case errors.Is(err, context.Canceled):
	log.Println("被取消了")
}
```

语义（可据此判断数据处于什么状态）：

| 场景 | 数据状态 |
| --- | --- |
| 导入被取消 | 已 `Commit` 的批次**保留**，当前未提交的那一批**回滚**（默认批大小 500 行，`WithBatchSize` 可调） |
| 导入正常失败（如唯一索引冲突） | 同上：之前提交的批次保留，出错批次回滚 |
| 导出被取消 | 输出文件被删掉，不留半截产物 |
| `WithReplace()` + 取消 | 旧表已经被 `DROP`，新表里只有已提交的批次 —— 此时表是「不完整」的，需要重跑 |

所以对「要么全有要么全无」的场景，**别靠 `WithReplace` 的中断语义**：要么把 `WithBatchSize` 调大到一个事务装得下，
要么导入到临时表再 `RENAME` 切换（这两步都建议配上足够宽的超时）。

命令行版对应的参数是 `-timeout`（见 `../cmd/cmd/README.md`），Ctrl+C 等价于取消。

---

## 三、数据库连接

```go
// 默认：SQLite + data.db + 单连接
db, err := file2db.Open("sqlite", "data.db", file2db.WithMaxOpenConns(1))
if err != nil {
	log.Fatal(err)
}
defer db.Close()

// 内存库（测试 / 临时分析）：每个连接一个独立库，包内强制单连接
mem, _ := file2db.OpenMemory()

// 换库只改驱动与 DSN（别忘了空导入对应驱动）
mysqlDB, _ := file2db.Open("mysql", "user:pass@tcp(127.0.0.1:3306)/db?charset=utf8mb4&parseTime=true")
pgDB, _ := file2db.Open("postgres", "postgres://user:pass@127.0.0.1:5432/db?sslmode=disable")
```

SQLite 驱动 `modernc.org/sqlite` 由本包空导入（驱动名 `sqlite`）；MySQL / Postgres 需要自己空导入：

```go
import (
	_ "github.com/go-sql-driver/mysql" // 驱动名 "mysql"
	_ "github.com/lib/pq"              // 驱动名 "postgres"
)
```

`Open` 会执行方言自带的初始化语句，SQLite 是这几条 PRAGMA：

```sql
PRAGMA journal_mode = WAL;    PRAGMA synchronous = NORMAL;
PRAGMA cache_size = -64000;   PRAGMA temp_store = MEMORY;
PRAGMA busy_timeout = 10000;
```

追加自己的语句用 `WithSQLitePragmas(...)` / `WithSetup(...)`（后者不限于 PRAGMA，如 MySQL 的 `SET NAMES utf8mb4`）。

**复用外部连接池**：`*sql.DB`、`*sqlx.DB` 都满足 `Database` 接口，可以直接传给导入 / 导出函数；
想连方言一起带上就用 `Wrap`：

```go
raw, _ := sql.Open("sqlite", "data.db")
raw.SetMaxOpenConns(1)
db := file2db.Wrap(raw, file2db.SQLite) // 第二个参数传 nil 时按驱动名自动推断
defer db.Close()

res, err := file2db.ExportTable(ctx, db, "sheet1", "out/users.csv")
```

方言推断顺序：目标库实现了 `Dialect()`（本包 `*DB` 即是）> `DriverName()`（`*sqlx.DB`）> 底层驱动类型 > 兜底 SQLite。
推不出来时用 `WithDialect` / `Wrap(db, file2db.MySQL)` / `WithImportDialect` / `WithExportDialect` 显式指定。

`Database` 接口要求实现方同时提供 `BeginTx(ctx, opts)`（导入用它开事务，才能被 ctx 取消）；
`*sql.DB`、`*sqlx.DB`、本包 `*DB` 都已经满足，自定义实现照抄这三个方法即可。

### 元数据助手

```go
tables, _ := file2db.Tables(ctx, db)                // 全部用户表名
ok, _ := file2db.TableExists(ctx, db, "sheet1")
n, _ := file2db.CountRows(ctx, db, "sheet1")
cols, _ := file2db.Columns(ctx, db, "sheet1")       // 表的列名
_ = file2db.DropTable(ctx, db, "sheet1")            // DROP TABLE IF EXISTS
```

---

## 四、API 一览

数据源与导入：

| 函数 | 说明 |
| --- | --- |
| `Imports(ctx, paths []string, dbOpts []DBOption, readOpts []Option, impOpts []ImportOption) (*DB, error)` | 打开库 + 批量导入 |
| `Import(ctx, db Database, paths []string, readOpts []Option, impOpts []ImportOption) ([]*Result, error)` | 只导入，返回每个文件的 `Result` |
| `NewSource(path string, opts ...Option) (Source, error)` | 按扩展名自动选数据源 |
| `NewSources(paths []string, opts ...Option) ([]Source, error)` | 批量创建（失败时关闭已打开的） |
| `CloseAll(sources []Source) error` | 关闭全部数据源 |
| `DetectKind(path string) (SourceKind, error)` | `excel` / `csv` / `tsv`（`.txt` 按 csv） |
| `NewExcel / NewTSV / NewCSV / NewSlice` | 三种数据源的构造函数 |
| `(*WorkBook) Import/Close`、`(*CSVSource) Import/Close/Encoding`、`(*SliceSource) Import/Reset/Len/Close` | 数据源方法（`Import` 都收 ctx） |

导出：

| 函数 | 说明 |
| --- | --- |
| `ExportTable(ctx, db Database, table, outputPath string, opts ...ExportOption) (*ExportResult, error)` | 单表 → 单文件；格式按后缀推断 |
| `ExportTables(ctx, db Database, outputDir string, opts ...ExportOption) ([]*ExportResult, error)` | 多表 → 目录（或 `WithExportSingleFile` 合成一个 xlsx） |
| `ExportSlice(ctx, db Database, table string, opts ...ExportOption) ([][]string, error)` | 多表 → 内存切片，第 0 行是列名 |
| `ExportFormat`（`FormatCSV` / `FormatXLSX`）、`ExportFormat.Ext()` | 导出格式 |

结果类型：

```go
type Result struct {        // 导入结果
	Table   string   // 只有一张表时等于 Tables[0]
	Tables  []string // 本次写入的所有表名（多 Sheet 多于一个）
	Columns []string // 清洗后的列名（多表时是最后一张表的）
	Rows    int64    // 成功写入的总行数
	Skipped int64    // 跳过的空行总数
	Indexes []string // 建好的索引名
	Elapsed time.Duration
}

type ExportResult struct {  // 导出结果
	Table   string
	Path    string   // 单文件模式下多张表是同一个路径
	Sheet   string   // Excel 实际写入的 Sheet 名（CSV 为空）
	Columns []string
	Rows    int64
	Elapsed time.Duration
}
```

纯函数（列名 / 表名处理）：

| 函数 | 说明 |
| --- | --- |
| `CleanHeaders(headers []string, keepRaw bool) []string` | 清洗列名（唯一入口：去重、避让 `id`、补 `colN`） |
| `SanitizeHeader(h string) string` / `RawHeader(h string) string` / `SanitizeHeaders(hs []string) []string` | 单列 / 批量 |
| `SanitizeTableName(s string) string` | 表名安全化 |
| `TableNameFromPath(path string) string` | 由文件名推导表名 |
| `HexPreview(path string, n int) (string, error)` | 调试用：文件头十六进制预览，排查编码问题 |

---

## 五、行为细则与坑

- **默认全 `TEXT`**：不做类型推断是刻意选择 —— `0000123`、`460-01` 这类值不会被改格式。
- **空行**：默认跳过并计入 `Result.Skipped`；表头之前的空行不计入 `HeaderRow` 的计数。
- **行长度不齐**：Excel 稀疏行 / CSV 行尾缺列会补齐（多余列截断）到表头列数，不会因参数个数不匹配报错。
- **事务**：一个 Sheet / 一个 CSV 各自开事务，每 `BatchSize` 行提交一次并重新预编译；
  事务不会跨表复用（复用会报 `transaction has already been committed or rolled back`）。
- **导出不带 `id` 再回灌**：导入时会固定加 `id INTEGER PRIMARY KEY`，所以「带 `id` 导出再导入」会让该列改名成 `id_2`；
  要来回导的表建议 `WithExportIndex(false)`。
- **`ExportTable` 不会创建输出目录**（目录不存在会报 `The system cannot find the path specified`）；
  `ExportTables` 会自动 `MkdirAll`。
- **文件库用单连接**：`WithMaxOpenConns(1)` 是默认值。导入时事务独占连接，
  多连接容易互相等锁；需要并发读可以自己调大，但要清楚 SQLite 的写锁行为。
- **CSV 编码**：`gbk` / `gb2312` / `gb18030` 统一用 GB18030 解码器（GBK 超集），
  所以 `CSVSource.Encoding()` 可能返回 `gb18030`。
- **`database/sql` 扫 NULL**：导出内部用 `[]any` + `anyToString` 自己转换，
  不会出现 `converting NULL to string is unsupported`；自己查表时注意给字段用 `sql.Null*` 或 `*string`。

---

## 六、测试

```bash
cd excelizev4        # 模块根目录（go.mod 所在处）
go test ./file2db/          # 全量，约 3 秒
go test -race ./file2db/    # 竞态检测（取消相关的用例会并发取消）
go test -cover ./file2db/   # 覆盖率，当前约 89%
```

测试**不依赖仓库里的二进制样本**：xlsx 用 `excelize` 在 `t.TempDir()` 里现场生成，
CSV 用 `os.WriteFile` 现写，跑完自动清理，不会往仓库里留文件。

覆盖的关键行为：

| 分组 | 覆盖点 |
| --- | --- |
| 编码探测 | UTF-8 / UTF-8 BOM / GBK(GB18030) / UTF-16LE / UTF-16BE、编码名别名、空文件、不支持的编码、`HexPreview` |
| 列名与表名 | 清洗（小写化、标点转下划线、BOM）、`WithRawHeader`、重名去重（大小写不敏感）、避让 `id`、空列名补 `colN`、数字开头加 `sheet_`、索引名截断不切坏多字节 |
| 方言 | 三种方言的建表 / 插入（`?` 与 `$n`）/ 建索引语句、标识符转义、方言推断四档优先级、MySQL 无 `IF NOT EXISTS` 的分支 |
| 表头定位 | `HeaderRow`（按非空行计数）、`SkipRows`、`NoHeader`、表头前空行、提前 EOF |
| CSV 导入 | GBK 自动识别与强制编码、TSV、自定义分隔符、注释行、前导空白、行尾缺列补齐 / 多余列截断、空行跳过与保留 |
| Excel 导入 | 稀疏行补齐、多 Sheet 一表一表、`WithSheets` + `WithSkipSheets` 组合（回归）、Sheet 名写错时的可用清单、`WithOnlyActiveSheet`、空表跳过与全空报错、数字开头的 Sheet 名、损坏文件 |
| 写入语义 | 追加 vs `WithReplace`、`WithBatchSize` 分批提交后重新预编译、`WithColumnTypes`、`WithCreateTable(false)`、`WithDefaultTable` |
| 索引 | 普通 / 唯一 / 自定义名、列名对不上时跳过、唯一索引冲突报错时数据已提交 |
| 取消 | 预先取消（不留表）、中途取消（已提交行数必为 `BatchSize` 的整数倍，证明回滚完整） |
| 导出 | CSV（含 GBK / BOM / 自定义分隔符 / 前导 0）、XLSX、多表一目录、多表一个 xlsx、`ExportSlice`、`NULL` → 空串、取消后不留半截文件、输出目录不存在 |
| 连接 | 默认单连接与 PRAGMA、追加 `WithSetup`、非法 DSN / 非法初始化语句报错、外部 `*sql.DB` / `*sqlx.DB` 直接可用、`Imports` 失败不交出连接 |

---

## 七、相关

- 命令行版：`../cmd/cmd`（`file2db-cli`）—— 支持参数与 JSON / YAML / TOML 配置文件，
  也是本包用法的一份完整参考实现，见 `../cmd/cmd/README.md`。
- 依赖：`github.com/xuri/excelize/v2`（Excel）、`modernc.org/sqlite`（SQLite）、`golang.org/x/text`（编码转换）。
