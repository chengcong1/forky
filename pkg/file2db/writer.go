package file2db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"log"
	"strings"
	"time"
	"unicode/utf8"
)

// rowReader 逐行读取单元格：返回下一行数据，读完返回 io.EOF。
//
// Excel 用 excelize 的行迭代器包装，CSV 用 encoding/csv 包装，
// 这样「定位表头 + 建表 + 批量写入」这套逻辑两边都能复用。
type rowReader func() ([]string, error)

// readHeaderRows 按 readConfig 定位表头。
//
// 返回值：
//   - headers：原始表头（未清洗）；为 nil 表示整个数据源没有一行非空数据；
//   - pending：无表头模式下已经读出来的第一行数据，调用方要把它当数据处理；
//   - err：真正读取失败时才非 nil。
func readHeaderRows(next rowReader, cfg readConfig) (headers, pending []string, err error) {
	nonBlank := 0
	for {
		cols, err := next()
		if errors.Is(err, io.EOF) {
			break // 读到文件尾也没找到非空行
		}
		if err != nil {
			return nil, nil, fmt.Errorf("读取表头失败: %w", err)
		}
		if isBlankRow(cols) { // 表头之前的空行不计入行数
			continue
		}
		nonBlank++
		if cfg.NoHeader {
			// 无表头模式：第一行非空行就是数据，列名自动生成 col1..colN
			return autoHeader(len(cols)), cols, nil
		}
		if nonBlank < cfg.HeaderRow {
			continue
		}
		// 表头之后仍需跳过的行（小计 / 说明行等），按“物理行”跳过
		for i := 0; i < cfg.SkipRows; i++ {
			if _, err := next(); err != nil {
				break
			}
		}
		return cols, nil, nil
	}
	return nil, nil, nil
}

// writeRows 是各数据源共用的写入端：建表、预编译、分批提交事务。
//
// 参数：
//   - ctx：控制超时与取消。每写一行都会检查一次，取消时立即返回 ctx 的错误，
//     当前未提交的那一批由 defer 回滚（之前已提交的批次会保留）；
//   - db：目标数据库（*DB 会自带的方言）；
//   - d：目标库方言，建表 / 插入语句按它生成；
//   - label：出错信息里的数据源描述，如 `工作表 "Sheet1"`、`CSV "a.csv"`；
//   - table：已经过 SanitizeTableName 的表名；
//   - headers：已经过 CleanHeaders 的列名；
//   - next：继续读取数据行，读到结尾返回 io.EOF；
//   - pending：无表头模式下已读出的第一行数据，可为 nil。
func writeRows(ctx context.Context, db Database, d Dialect, label, table string, headers []string, cfg ImpOptions, next rowReader, pending []string) (*Result, error) {
	start := time.Now()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if cfg.Replace {
		if _, err := db.ExecContext(ctx, "DROP TABLE IF EXISTS "+d.QuoteIdent(table)); err != nil {
			return nil, fmt.Errorf("删除旧表 %s 失败: %w", table, err)
		}
	}

	// 一个数据源一个事务：事务会独占连接池里的唯一连接（默认 MaxOpenConns=1），
	// 建表 / 预编译 / 插入必须全部走同一个 tx，否则会互相等锁。
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("开启事务失败: %w", err)
	}
	var (
		stmt *sql.Stmt
		done bool // 事务已提交，避免 defer 里重复 Rollback
	)
	// 注意：不能直接写 `defer stmt.Close()`，那样会在注册时就把当时的 nil
	// 绑定进延迟调用；用闭包才能在函数返回时读取“最新”的 stmt 并判空。
	defer func() {
		if stmt != nil {
			stmt.Close()
		}
		if !done {
			_ = tx.Rollback()
		}
	}()

	insertSQL := buildInsertSQL(d, table, headers)
	if cfg.CreateTable {
		createSQL := buildCreateTableSQL(d, table, headers, cfg.ColumnTypes, cfg.DefaultType)
		if _, err := tx.ExecContext(ctx, createSQL); err != nil {
			return nil, fmt.Errorf("建表 %s 失败: %w", table, err)
		}
	}
	if stmt, err = tx.PrepareContext(ctx, insertSQL); err != nil {
		return nil, fmt.Errorf("预编译插入语句失败: %w", err)
	}

	var (
		inserted int64
		skipped  int64
		line     int64 // 已读到的数据行序号，仅用于报错定位
	)
	insert := func(cols []string) error {
		line++
		// 每行都查一次 ctx：取消 / 超时能立刻停下，不必等整个文件读完。
		// ctx.Err() 对未取消的 context 只是一次类型判断，开销可以忽略。
		if err := ctx.Err(); err != nil {
			return err
		}
		if !cfg.KeepBlankRows && isBlankRow(cols) {
			skipped++
			return nil
		}
		// Excel 稀疏存储 / CSV 行尾缺列：行的单元格数可能小于表头列数，
		// 参数不够会报 "missing argument with index N"，
		// 所以这里先把每行补齐 / 截断到与列数一致。
		if _, err := stmt.ExecContext(ctx, normalizeRow(cols, len(headers))...); err != nil {
			return fmt.Errorf("%s 第 %d 行数据写入失败: %w", label, line, err)
		}
		inserted++
		if cfg.ProgressEvery > 0 && cfg.Logger != nil && inserted%cfg.ProgressEvery == 0 {
			cfg.Logger.Printf("file2db: %s 已写入 %d 行", table, inserted)
		}
		// 每 BatchSize 行提交一次事务，并在新事务里重新预编译
		if inserted%int64(cfg.BatchSize) == 0 {
			if err := tx.Commit(); err != nil {
				return fmt.Errorf("提交事务失败: %w", err)
			}
			if tx, err = db.BeginTx(ctx, nil); err != nil {
				return fmt.Errorf("重启事务失败: %w", err)
			}
			stmt.Close()
			// 重编译必须用表头列名，不能拿当前数据行的值当列名
			if stmt, err = tx.PrepareContext(ctx, insertSQL); err != nil {
				return fmt.Errorf("预编译插入语句失败: %w", err)
			}
		}
		return nil
	}

	if pending != nil { // 无表头模式：已经读出来的第一行就是数据
		if err := insert(pending); err != nil {
			return nil, err
		}
	}
	for {
		cols, err := next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%s 读取第 %d 行失败: %w", label, line+1, err)
		}
		if err := insert(cols); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("提交事务失败: %w", err)
	}
	done = true

	// 数据全部落库后再建索引：数据库一次性建索引比逐行维护快得多，
	// 所以 WithIndex 系列选项是「写完再建」，而不是建表时就带上。
	indexes, err := createIndexes(ctx, db, d, table, headers, cfg.Indexes, cfg.Logger, inserted)
	if err != nil {
		return nil, err
	}

	return &Result{
		Table:   table,
		Tables:  []string{table},
		Columns: headers,
		Rows:    inserted,
		Skipped: skipped,
		Indexes: indexes,
		Elapsed: time.Since(start),
	}, nil
}

// createIndexes 在导入完成后为表建索引，返回建好的索引名。
//
// 规则：
//   - 表里没写入任何数据时跳过（空表建索引没意义）；
//   - 用户写的列名会先对齐到表里真实的列名，对不上的列忽略；
//     多 Sheet 导入时，某张表没有这些列就跳过这张表的这个索引；
//   - 不支持的方言（MySQL）先查 information_schema 再建，避免报「索引已存在」。
func createIndexes(ctx context.Context, db Database, d Dialect, table string, headers []string, specs []IndexSpec, logger *log.Logger, inserted int64) ([]string, error) {
	if len(specs) == 0 || inserted == 0 {
		return nil, nil
	}

	created := make([]string, 0, len(specs))
	for _, spec := range specs {
		cols := resolveIndexColumns(headers, spec.Columns)
		if len(cols) == 0 {
			if logger != nil {
				logger.Printf("file2db: 表 %s 上没有列 %v，跳过该索引", table, spec.Columns)
			}
			continue
		}
		name := spec.Name
		if name == "" {
			name = defaultIndexName(table, cols)
		} else {
			name = SanitizeTableName(name)
		}

		if !d.IndexIfNotExists() {
			exists, err := indexExists(ctx, db, d, table, name)
			if err != nil {
				return created, fmt.Errorf("检查索引 %s 失败: %w", name, err)
			}
			if exists {
				continue
			}
		}

		statement := buildCreateIndexSQL(d, table, name, cols, spec.Unique)
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return created, fmt.Errorf("表 %s 建索引 %s 失败: %w", table, name, err)
		}
		created = append(created, name)
		if logger != nil {
			logger.Printf("file2db: %s 已建索引 %s(%s)", table, name, strings.Join(cols, ", "))
		}
	}
	return created, nil
}

// indexExists 查询索引是否已存在（用于不支持 CREATE INDEX IF NOT EXISTS 的方言）。
func indexExists(ctx context.Context, db Database, d Dialect, table, name string) (bool, error) {
	query, args := d.IndexExists(table, name)
	var n int
	if err := db.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

// buildCreateIndexSQL 生成建索引语句；支持 IF NOT EXISTS 的方言带上它。
func buildCreateIndexSQL(d Dialect, table, name string, cols []string, unique bool) string {
	kind := "INDEX"
	if unique {
		kind = "UNIQUE INDEX"
	}
	guard := ""
	if d.IndexIfNotExists() {
		guard = "IF NOT EXISTS "
	}
	return fmt.Sprintf("CREATE %s %s%s ON %s (%s)",
		kind, guard, d.QuoteIdent(name), d.QuoteIdent(table), quoteColumnList(d, cols))
}

// resolveIndexColumns 把用户写的列名对齐到表里真实的列名：
// 原始表头（"AGE"）、清洗后的列名（"age"）都能命中，对不上的忽略，并去掉重复列。
func resolveIndexColumns(headers, want []string) []string {
	lookup := make(map[string]string, len(headers)*2)
	for _, h := range headers {
		lookup[strings.ToLower(h)] = h
		lookup[SanitizeHeader(h)] = h
	}
	out := make([]string, 0, len(want))
	seen := make(map[string]struct{}, len(want))
	for _, c := range want {
		name, ok := lookup[strings.ToLower(strings.TrimSpace(c))]
		if !ok {
			name, ok = lookup[SanitizeHeader(c)]
		}
		if !ok {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

// defaultIndexName 生成默认索引名 idx_<表名>_<列名...>。
// PostgreSQL 标识符上限 63 字节、MySQL 64 字符，超长时按字节截断并附 CRC32 后缀，
// 避免两个索引截断后撞名。
func defaultIndexName(table string, cols []string) string {
	name := SanitizeTableName("idx_" + table + "_" + strings.Join(cols, "_"))
	const maxBytes = 60
	if len(name) > maxBytes {
		prefix := truncateBytes(name, maxBytes-9)
		name = fmt.Sprintf("%s_%08x", prefix, crc32.ChecksumIEEE([]byte(name)))
	}
	return name
}

// truncateBytes 按字节截断字符串，且不切断多字节字符。
func truncateBytes(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	for limit > 0 && !utf8.RuneStart(s[limit]) {
		limit--
	}
	return s[:limit]
}

// buildCreateTableSQL 生成建表语句：
// 自增主键列定义、标识符引用、建表尾巴（MySQL 的字符集）都取自方言。
func buildCreateTableSQL(d Dialect, table string, headers []string, types map[string]string, defaultType string) string {
	defs := make([]string, 0, len(headers)+1)
	defs = append(defs, d.AutoIncrementPK())
	for _, h := range headers {
		t := defaultType
		if custom, ok := types[h]; ok && custom != "" {
			t = custom
		}
		defs = append(defs, fmt.Sprintf("%s %s", d.QuoteIdent(h), t))
	}
	return fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (%s)%s",
		d.QuoteIdent(table), strings.Join(defs, ", "), d.CreateTableSuffix())
}

// buildInsertSQL 生成 INSERT 语句：占位符按方言生成（SQLite/MySQL 是 ?，Postgres 是 $n）。
func buildInsertSQL(d Dialect, table string, headers []string) string {
	placeholders := make([]string, len(headers))
	for i := range placeholders {
		placeholders[i] = d.BindVar(i)
	}
	return fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		d.QuoteIdent(table), quoteColumnList(d, headers), strings.Join(placeholders, ", "))
}
