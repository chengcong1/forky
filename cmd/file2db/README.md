# file2db-cli

`file2db` 包的命令行版：**Excel / CSV <-> 数据库**（默认 SQLite）。
既可以只用命令行参数，也可以用一个 **JSON / YAML / TOML** 配置文件描述整套流程。

```
cmd/cmd/
├── main.go     入口：解析参数 -> 打开数据库 -> 按命令执行
├── flags.go    命令行参数表 + 「默认值 < 配置文件 < 命令行」的覆盖逻辑
├── config.go   配置文件结构与默认值、JSON / YAML / TOML 加载、小工具（分隔符 / 列类型 / 索引解析）
├── run.go      import / export / query / tables 的具体执行与输出端（CSV / XLSX / 控制台）
├── example.yaml / example.toml / example.json   三种格式的配置模板（带注释，照抄改路径即可用）
└── README.md
```

## 构建与运行

```powershell
cd excelizev4            # 模块根目录（go.mod 所在处）
go build -o f2db.exe ./cmd/cmd
.\f2db.exe help          # 全部参数与当前默认值
```

也可以不编译，直接用 `go run ./cmd/cmd <命令> ...`。

## 命令

| 命令 | 别名 | 作用 |
| --- | --- | --- |
| `import` | `in` / `i` | 文件 -> 数据库（`.xlsx/.xlsm/.csv/.tsv/.txt`，按扩展名自动识别） |
| `export` | `out` / `e` | 数据库 -> CSV / XLSX（一表一文件，或 `-single-file` 合成一个 xlsx） |
| `query` | `sql` / `q` | 执行 SQL；结果写 CSV / XLSX，或对齐打印到控制台 |
| `tables` | `ls` | 列出库里的用户表与行数 |
| `version` / `help` | | 版本 / 帮助（`-h` 等效） |

不给命令时，按 **`import` -> `queries` -> `export`** 的顺序把配置文件里的任务跑一遍，
一个配置文件就能描述「拉数 -> 加工 -> 导出」的固定套路。

## 参数的三层覆盖关系

```
内置默认值   <   -config 指定的配置文件   <   命令行参数
```

- 配置文件支持 **JSON / YAML / TOML**，按扩展名自动识别（`.json` / `.yaml` / `.yml` / `.toml` / `.tml`），
  认不出来时用 `-config-format json|yaml|toml` 显式指定；**三种格式字段名完全一样**，只是 YAML / TOML 可以写注释；- 配置文件里**没写的字段保持默认值**，所以配置可以只写关心的几项；
- 字段名写错会直接报错（`json: unknown field "filesx"`），不会出现「改了却不生效」；
- 列表类参数（`-file` / `-sheet` / `-index` / `-tables`）在命令行给出时是**追加**，不会清掉配置文件里的值；
- `file2db-cli help` 打印的参数默认值就是「当前配置文件生效后的值」，一眼能看出最终配置。

## 超时、中断与退出码

```powershell
.\f2db.exe import -dsn big.db -file huge.csv -timeout 30m   # 定时任务里建议总是显式给
```

- `-timeout` 是**整次执行**的总时限，写法同 Go 的 duration（`30m`、`2h`、`90s`）；`0` 或留空表示不超时（默认）。
- `Ctrl+C` 与超时的效果一样：取消 context。
  - 导入：停在「当前批次」边界上，**当前批次回滚、已提交批次保留**；
  - 导出 / 查询：立即停止，导出会删掉半截产物，不留坏文件。
- 注意 `-replace` + 中断的组合：旧表已被 `DROP`，新表只有已提交的批次，**结果是不完整的表**。
  对「要么全有要么全无」的导入，请把 `-batch` 调大到一个事务装得下，或导到临时表再改名。

| 退出码 | 含义 |
| --- | --- |
| `0` | 成功 |
| `1` | 执行失败（SQL 错误、文件读不了、表不存在等） |
| `2` | 参数 / 配置文件有误（含 `-timeout` 写法不对） |
| `124` | 超过 `-timeout`，任务被中止 |
| `130` | 被 `Ctrl+C` 中断 |

## 常用示例

```powershell
# 1) 参数全写命令行：文件数据导进 SQLite（替换旧表、保留原始表头、跳过辅助 Sheet、建索引）
.\f2db.exe import -dsn data.db -replace -raw-header `
    -skip-sheet Index -skip-sheet HideEnum -skip-rows 4 `
    -index name -file data.xlsx -file data.csv

# 2) 用一个配置文件跑完整流程（三种格式任选）
.\f2db.exe -config task.yaml
.\f2db.exe -config task.toml

# 3) 只看某张表（控制台对齐打印，适合几十行的抽查）
.\f2db.exe query -dsn data.db -sql "SELECT * FROM users LIMIT 30"

# 4) 查询结果落文件：后缀决定格式，csv 用 gbk + BOM 让 Excel 直接双击可看
.\f2db.exe query -dsn data.db -sql "SELECT * FROM users" -sql-out out/users.xlsx -sql-sheet users
.\f2db.exe query -dsn data.db -sql "SELECT * FROM users" -sql-out out/users.csv -out-encoding gbk -bom

# 5) 导出全库成一个 xlsx（每表一个 Sheet，不带自增主键列）
.\f2db.exe export -dsn data.db -out out -format xlsx -single-file users-all.xlsx -export-index=false

# 6) 换库只改驱动与 DSN（mysql / postgres 驱动已内置）
.\f2db.exe export -driver mysql -dsn "user:pass@tcp(127.0.0.1:3306)/db?charset=utf8mb4" -out out
```

## 配置文件

支持三种格式，**字段名完全一样**（就是下面各表里列的名字），按 `-config` 的扩展名自动识别：

| 格式 | 扩展名 | 特点 |
| --- | --- | --- |
| YAML | `.yaml` / `.yml` | 可写注释，多行 SQL 可以直接用块标量（`sql:` + 缩进），可读性最好 |
| TOML | `.toml` | 可写注释，表结构清晰（`[import]` / `[[queries]]`） |
| JSON | `.json` | 最通用，但**不能写注释** |

模板：`example.yaml`、`example.toml`、`example.json`（三者等价，复制一份改路径即可）。

```powershell
.\f2db.exe -config task.yaml                        # 按扩展名识别
.\f2db.exe -config task.conf -config-format yaml     # 扩展名不认识时显式指定
```

实现上 YAML / TOML 会先解成通用 map 再转成等价的 JSON，复用同一条解析路径，
所以「字段名拼错会报错」「`indexes` 支持字符串 / 数组 / 对象三种写法」这些行为在三种格式里完全一致。

相对路径按**当前工作目录**解析（`query.sqlFile` 例外，按配置文件所在目录解析）。

### `db`

| 字段 | 默认 | 说明 |
| --- | --- | --- |
| `driver` | `sqlite` | `sqlite` / `mysql` / `postgres`（已内置驱动，方言自动适配） |
| `dsn` | `data.db` | SQLite 是库文件路径或 `:memory:`；其它库写各自 DSN |
| `maxOpenConns` | `1` | 文件库保持 1 最稳（导入时事务要独占连接） |
| `setup` | 无 | 建连后额外执行的语句，如 `SET NAMES utf8mb4` |

### `import`（文件 -> 数据库）

| 字段 | 默认 | 说明 |
| --- | --- | --- |
| `files` | 无 | 要导入的文件列表，可混合 xlsx / csv |
| `table` | 由数据源推导 | 目标表名；**只有单表数据源**（CSV 或单个 Sheet）时生效 |
| `replace` | `false` | 导入前 `DROP TABLE`，即全量覆盖 |
| `createTable` | `true` | 自动建表 |
| `batchSize` | `500` | 每条事务写入的行数 |
| `defaultType` | `TEXT` | 未指定列的默认类型（TEXT 最安全，前导 0 不会被改格式） |
| `columnTypes` | 无 | 指定列类型，如 `{"age":"INTEGER"}` |
| `indexes` | 无 | 导入完成后建索引，三种写法见下 |
| `keepBlankRows` | `false` | 是否保留全空行 |
| `progress` | `0` | 每多少行打一次进度日志，0 关闭 |
| `sheets` / `skipSheets` | 无 | Excel：只读 / 跳过哪些 Sheet |
| `headerRow` | `1` | 第几个**非空行**作为表头 |
| `skipRows` | `0` | 表头之后再跳过几行（小计行、说明行） |
| `noHeader` | `false` | 无表头，列名自动生成 `col1..colN` |
| `rawHeader` | `false` | 保留原始表头（不做小写化与下划线替换） |
| `encoding` | `auto` | CSV 编码：`auto/utf-8/gbk/gb18030/utf-16le/utf-16be` |
| `delimiter` | `,` | CSV 分隔符，制表符写 `"\t"` |
| `lazyQuotes` | `true` | 容忍不规范的引号（网管导出的脏数据常见） |
| `comment` | 无 | 注释字符，以其开头的行忽略 |
| `trimLeadingSpace` | `false` | 去掉字段前导空白 |
| `fieldsPerRecord` | `-1` | 每行列数限制：`0` 按表头、`-1` 不限制 |

`indexes` 的三种写法（命令行对应 `-index 列1,列2`、`-unique-index 列`）：

```json
"indexes": ["Name"]
"indexes": ["Age,Sex"]
"indexes": [{ "name": "idx_age", "columns": ["age"], "unique": true }]
```

### `queries`（执行 SQL）

| 字段 | 默认 | 说明 |
| --- | --- | --- |
| `name` | 无 | 只用于日志 |
| `sql` / `sqlFile` | 无 | 内联 SQL 或从文件读（`sql` 优先） |
| `out` | 控制台 | 输出文件 `.csv` / `.xlsx`；留空打印到控制台 |
| `format` | 按后缀推断 | `csv` / `xlsx` / `table`；控制台默认 `table` |
| `sheet` | `Sheet1` | 写 xlsx 时的 Sheet 名 |
| `encoding` | 继承 `export.encoding` | 输出编码 |
| `delimiter` | 继承 `export.delimiter` | 输出分隔符 |
| `bom` | `false` | CSV 是否写 BOM |
| `noHeader` | `false` | 不写表头行 |

- 非查询语句（`INSERT/UPDATE/DELETE/DDL`）走 `Exec`，只打印影响行数；
- 命令行给了 `-sql` / `-sql-file`（或把 SQL 直接写在命令后面）时**只跑这一条**，
  配置文件里的 `queries` 会被忽略，避免混在一起分不清输出去哪。

### `export`（数据库 -> 文件）

| 字段 | 默认 | 说明 |
| --- | --- | --- |
| `dir` | 无 | 输出目录，一表一个文件 |
| `tables` | 全部用户表 | 只导出指定表 |
| `format` | 按目录/后缀 | `csv` / `xlsx` |
| `singleFile` | 无 | 多张表写进同一个 xlsx（每表一个 Sheet） |
| `index` | `true` | 是否导出自增主键 `id` 列 |
| `encoding` | `utf-8` | CSV 输出编码 |
| `delimiter` | `,` | CSV 输出分隔符 |
| `bom` | `false` | CSV 是否写 BOM |
| `progress` | `0` | 每多少行打一次进度 |

### `quiet`

`true` 表示不再输出导入 / 导出的**进度日志**；工具自己的汇总行（`导入完成：...`、`导出完成：...`）仍会打印，因为那就是执行结果。

### `timeout`

整次执行的总超时，字符串写法同 Go 的 duration（`"30m"`、`"2h"`），`"0"` 或留空表示不超时（默认）。
放在配置里适合定时任务：任务卡住时会被自动中止，不会一直占着调度位。

```json
{
  "timeout": "2h",
  "db": { "dsn": "data.db" },
  "import": { "files": ["big.xlsx"], "replace": true }
}
```

## 行为上的几个要点

- **表名**：Excel 一个 Sheet 一张表（默认取 Sheet 名），CSV/TSV 一个文件一张表（默认取文件名）；
  多 Sheet 时 `table` 会被忽略（不同结构混进同一张表没有意义）。
- **列名**：默认清洗成小写下划线（`创建时间 (UTC)` -> `创建时间_utc`）；加 `rawHeader`
  后原样保留，查询时用双引号包裹即可（`SELECT "创建时间 (UTC)" FROM ...`）。
- **回灌**：导入时会自动加一列自增主键 `id`，所以「带 id 导出再导入」会让该列被改名成 `id_2`；
  要来回导的表建议 `"index": false`。
- **`table` 格式会整表进内存**（为了算列宽对齐），只适合人工抽查；大结果用 `-format csv` 流式输出。
- 一条 SQL 一次执行；多个语句请拆成多条 `queries`，或写进 SQL 文件后逐条配置。
- 换库只需改 `driver` / `dsn`，`file2db` 会按驱动名自动选方言（标识符引用、占位符、元数据查询都不会写错）。
