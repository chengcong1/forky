package file2db

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
)

// WorkBook 基于 excelize 的行迭代器流式读取 xlsx/xlsm 文件，
// 不会把整张表载入内存。
//
// 一个 WorkBook 代表一份工作簿：NewExcel 时就把要处理的工作表名记下来，
// Import 时逐个工作表流式导入 SQLite（一个 Sheet 一张表，表名默认为 Sheet 名）。
type WorkBook struct {
	file   *excelize.File
	sheets []string
	cfg    readConfig
}

// type WorkSheet struct {
// it    *excelize.Rows
// sheet string
// cur   rowCursor // 表头定位 + 逐行读取
// }

// NewExcel 打开 Excel 文件并创建流式数据源，用完请 Close。
func NewExcel(path string, opts ...ExcelOption) (*WorkBook, error) {
	cfg := defaultReadConfig()
	for _, o := range opts {
		o(&cfg)
	}

	f, err := excelize.OpenFile(path, excelize.Options{RawCellValue: cfg.RawCellValue})
	if err != nil {
		return nil, fmt.Errorf("file2db: 打开 Excel 失败 %s: %w", path, err)
	}

	list := f.GetSheetList()
	if len(list) == 0 {
		f.Close()
		return nil, fmt.Errorf("file2db: Excel 中没有工作表: %s", path)
	}
	if len(cfg.Sheets) == 0 && cfg.OnlyActiveSheet {
		// OnlyActiveSheet 只在没指定 WithSheets 时生效：显式点名了就照点名的来
		idx := f.GetActiveSheetIndex()
		if idx < 0 || idx >= len(list) {
			idx = 0
		}
		cfg.Sheets = []string{list[idx]}
	} else {
		// 没点名就取全部；点了名也只保留工作簿里真实存在的，再减掉要跳过的。
		// 两种情况都要过 handleUseSheet —— 否则「WithSheets + WithSkipSheets」
		// 会静默丢掉 SkipSheets，写错的 Sheet 名也会拖到 Rows() 才报错。
		cfg.Sheets = handleUseSheet(list, cfg.Sheets, cfg.SkipSheets)
	}
	if len(cfg.Sheets) == 0 {
		f.Close()
		return nil, fmt.Errorf("file2db: 没有符合条件的工作表: %s（可用: %s）",
			path, strings.Join(list, ", "))
	}
	return &WorkBook{
		file:   f,
		sheets: cfg.Sheets,
		cfg:    cfg,
	}, nil
}

/*
逻辑说明：
useSheet 为空：遍历 allSheet，过滤掉 skipSheet 中存在的项，返回剩余部分。
useSheet 不为空：遍历 allSheet，只保留同时满足"在 useSheet 中"且"不在 skipSheet 中"的项。
使用 map[string]struct{} 作为集合，查找时间复杂度为 O(1)。
*/
func handleUseSheet(allSheet, useSheet, skipSheet []string) []string {
	// 构建 skipSheet 的集合，方便快速查找
	skipSet := make(map[string]struct{}, len(skipSheet))
	for _, s := range skipSheet {
		skipSet[s] = struct{}{}
	}

	// 如果 useSheet 为空，返回 allSheet 中不在 skipSheet 里的项
	if len(useSheet) == 0 {
		result := make([]string, 0, len(allSheet))
		for _, s := range allSheet {
			if _, ok := skipSet[s]; !ok {
				result = append(result, s)
			}
		}
		return result
	}

	// useSheet 不为空时，返回 useSheet 中在 allSheet 里存在且不在 skipSheet 里的项
	useSet := make(map[string]struct{}, len(useSheet))
	for _, s := range useSheet {
		useSet[s] = struct{}{}
	}

	result := make([]string, 0, len(useSheet))
	for _, s := range allSheet {
		if _, ok := useSet[s]; ok {
			if _, skip := skipSet[s]; !skip {
				result = append(result, s)
			}
		}
	}
	return result
}

func (wb *WorkBook) Close() error {
	return wb.file.Close()
}

// Import 把工作簿里配置好的工作表逐个流式导入 SQLite（一个 Sheet 一张表）。
//
// ctx 控制超时与取消：取消后正在写的那一批会回滚，已经提交的批次保留
// （批大小由 WithBatchSize 决定，默认 500 行一个事务）。
//
// 与旧实现的区别：
//   - 每个工作表各自开启 / 提交事务。事务一旦 Commit 就不能再用，
//     多表复用同一个 tx 就会报 “sql: transaction has already been committed or rolled back”；
//   - 表头行号单独用一个计数器（非空行计数），不再和“已导入行数”混用，
//     否则默认 HeaderRow=1 时所有行都会被当成表头前的行而全部跳过；
//   - 所有错误都通过返回值上报，不再 log.Fatal 直接杀掉进程；
//   - 表名 / 列名统一安全化，并支持 WithTable/WithReplace/WithCreateTable/
//     WithColumnTypes/WithKeepBlankRows/WithProgress 等选项。
func (wb *WorkBook) Import(ctx context.Context, db Database, opts ...ImportOption) (*Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if db == nil {
		return nil, errors.New("file2db: db 不能为 nil")
	}
	if wb.file == nil {
		return nil, errors.New("file2db: file 不能为 nil")
	}
	cfg := defaultOptions()
	for _, o := range opts {
		o(&cfg)
	}
	// WithTable 只对「单个工作表」有意义：多表共用一个表名会把结构不同的
	// 数据混进同一张表，所以多表时忽略它。
	if len(wb.sheets) != 1 {
		cfg.Table = ""
	}

	start := time.Now()
	total := &Result{}
	for _, sheet := range wb.sheets {
		res, err := wb.importSheet(ctx, db, sheet, cfg)
		if err != nil {
			total.Elapsed = time.Since(start)
			return total, fmt.Errorf("file2db: 导入工作表 %q 失败: %w", sheet, err)
		}
		if res == nil { // 空表：没有表头也没有数据，直接跳过
			continue
		}
		total.Tables = append(total.Tables, res.Table)
		total.Columns = res.Columns
		total.Rows += res.Rows
		total.Skipped += res.Skipped
		total.Indexes = append(total.Indexes, res.Indexes...)
	}
	total.Elapsed = time.Since(start)
	if len(total.Tables) == 1 {
		total.Table = total.Tables[0]
	}
	if len(total.Tables) == 0 {
		return total, errors.New("file2db: 所有工作表都是空表，没有导入任何数据")
	}
	return total, nil
}

// importSheet 导入单个工作表：表头定位、建表、批量事务插入。
// 返回 nil, nil 表示该工作表为空（读不到任何非空行），调用方应跳过它。
func (wb *WorkBook) importSheet(ctx context.Context, db Database, sheet string, cfg ImpOptions) (*Result, error) {
	rows, err := wb.file.Rows(sheet)
	if err != nil {
		return nil, fmt.Errorf("读取工作表失败: %w", err)
	}
	defer rows.Close()

	// 把 excelize 的行迭代器包装成通用的 rowReader，
	// 表头定位与写入全程共用同一个迭代器（readHeaderRows 会把光标停在第一行数据上）。
	next := func() ([]string, error) {
		if !rows.Next() {
			if err := rows.Error(); err != nil {
				return nil, err
			}
			return nil, io.EOF
		}
		return rows.Columns()
	}

	rawHeader, pending, err := readHeaderRows(next, wb.cfg)
	if err != nil {
		return nil, err
	}
	if rawHeader == nil {
		return nil, nil // 空表
	}

	table := cfg.Table
	if table == "" {
		table = sheet
	}
	table = SanitizeTableName(table)
	// 默认把列名清洗成小写下划线；WithRawHeader() 时原样保留表头文字
	headers := CleanHeaders(rawHeader, wb.cfg.KeepRawHeader)
	return writeRows(ctx, db, pickDialect(cfg.Dialect, db), fmt.Sprintf("工作表 %q", sheet), table, headers, cfg, next, pending)
}

// 表头定位、建表 / 插入语句与写入管线都在 writer.go：
// readHeaderRows / writeRows / buildCreateTableSQL / buildInsertSQL，由 Excel 与 CSV 共用。

// normalizeRow 把某一行的单元格补足/截断到与列数一致。
// Excel 稀疏存储导致行尾空单元格缺失，此处缺失部分用空串填充，避免参数个数不匹配。
func normalizeRow(cols []string, colCount int) []any {
	args := make([]any, colCount)
	for i := range args {
		if i < len(cols) {
			args[i] = cols[i]
		} else {
			args[i] = ""
		}
	}
	return args
}
