package file2db

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// Source 表示一个可按行流式导入 SQLite 的数据源。
//
// 约定：Import 会把数据源内容写入 db（自动建表 + 批量事务），
// 用完记得 Close 释放文件句柄；两者都由下面的 Import / Imports 统一编排。
//
// Import 必须尊重 ctx：超时或 Ctrl+C 时要尽快返回，并把当前未提交的那一批回滚。
type Source interface {
	Import(ctx context.Context, db Database, opts ...ImportOption) (*Result, error)
	Close() error
}

// 编译期断言：Excel 工作簿与 CSV 文件都已实现 Source。
var (
	_ Source = (*WorkBook)(nil)
	_ Source = (*CSVSource)(nil)
)

// ---------- 按扩展名自动选择数据源 ----------

// SourceKind 数据源类型。
type SourceKind string

const (
	KindExcel SourceKind = "excel"
	KindCSV   SourceKind = "csv"
	KindTSV   SourceKind = "tsv"
)

// DetectKind 根据文件扩展名判断数据源类型。
func DetectKind(path string) (SourceKind, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".xlsx", ".xlsm", ".xltx", ".xltm":
		return KindExcel, nil
	case ".csv":
		return KindCSV, nil
	case ".tsv", ".tab":
		return KindTSV, nil
	case ".txt":
		// 纯文本按 CSV 处理，分隔符可用 WithDelimiter 覆盖
		return KindCSV, nil
	}
	return "", fmt.Errorf("file2db: 不支持的文件类型 %s（支持 .xlsx/.xlsm/.csv/.tsv/.txt）", filepath.Ext(path))
}

// NewSource 按扩展名自动创建数据源（Excel / CSV / TSV）。
// 同一组 Option 会按需作用到对应数据源上。
func NewSource(path string, opts ...Option) (Source, error) {
	kind, err := DetectKind(path)
	if err != nil {
		return nil, err
	}
	if kind == KindExcel {
		return NewExcel(path, opts...)
	}
	if kind == KindTSV {
		return NewTSV(path, opts...)
	}
	return NewCSV(path, opts...)
}

// NewSources 按扩展名批量创建数据源（Excel / CSV / TSV）。
//
// 任一文件创建失败时会关闭前面已打开的数据源，避免文件句柄泄漏。
func NewSources(paths []string, opts ...Option) ([]Source, error) {
	sources := make([]Source, 0, len(paths))
	for _, path := range paths {
		s, err := NewSource(path, opts...)
		if err != nil {
			// 关闭失败的原因不覆盖真正的失败原因，只作为补充信息合并返回
			return nil, errors.Join(err, CloseAll(sources))
		}
		sources = append(sources, s)
	}
	return sources, nil
}

// CloseAll 关闭一组数据源并返回全部关闭错误的合并结果（nil 表示全部关闭成功）。
// 即使中途某个关闭失败，其余数据源仍会继续关闭。
func CloseAll(sources []Source) error {
	errs := make([]error, 0, len(sources))
	for _, s := range sources {
		if s == nil {
			continue
		}
		if err := s.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Import 把多个文件依次导入已打开的 db，返回每个文件（工作簿）的导入结果。
//
//   - ctx：控制超时与取消，任一文件被取消都会立即返回；
//   - readOpts 作用于数据源：表头行 / 跳过行 / Sheet 名 / 编码 / 分隔符等；
//   - impOpts 作用于写入端：表名 / 批量大小 / WithReplace / 列类型等。
//
// 表名：Excel 一个 Sheet 一张表（默认取 Sheet 名），CSV/TSV 一个文件一张表（默认取文件名）。
// 任一文件失败立即返回，前面已成功写入的表会保留。
func Import(ctx context.Context, db Database, paths []string, readOpts []Option, impOpts []ImportOption) (results []*Result, err error) {
	if db == nil {
		return nil, errors.New("file2db: db 不能为 nil")
	}
	if len(paths) == 0 {
		return nil, errors.New("file2db: paths 不能为空")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	sources, err := NewSources(paths, readOpts...)
	if err != nil {
		return nil, err
	}
	// 无论成功与否都要关闭数据源，避免文件句柄泄漏；
	// 关闭失败不覆盖主流程错误，用 errors.Join 合并返回。
	defer func() { err = errors.Join(err, CloseAll(sources)) }()

	results = make([]*Result, 0, len(sources))
	for i, src := range sources {
		res, ierr := src.Import(ctx, db, impOpts...)
		if ierr != nil {
			return results, fmt.Errorf("file2db: 导入 %s 失败: %w", paths[i], ierr)
		}
		results = append(results, res)
	}
	return results, nil
}

// Imports 一步完成「打开数据库 → 批量导入」，返回可直接查询的 *DB。
//
//	db, err := file2db.Imports(ctx,
//		[]string{"data.csv", "data.xlsx"},
//		[]file2db.DBOption{file2db.WithDSN("data.db")},
//		[]file2db.Option{file2db.WithSheets([]string{"sheet"})},
//		[]file2db.ImportOption{file2db.WithReplace()},
//	)
//	if err != nil {
//		log.Fatal(err)
//	}
//	defer db.Close()
//
// 表名默认取 Excel 的 Sheet 名或 CSV 的文件名，可用 WithTable 覆盖；
// 多个文件写到同名表时，需用 WithReplace 避免列结构冲突。
// 导入失败时会关闭数据库并返回 nil，避免调用方拿到半成品连接；
// 需要逐文件的写入行数 / 耗时，请直接用 Import。
//
// 换库只改驱动与 DSN，例如：
//
//	[]file2db.DBOption{file2db.WithDriveName("mysql"), file2db.WithDSN("user:pass@tcp(127.0.0.1:3306)/db?charset=utf8mb4")}
func Imports(ctx context.Context, paths []string, dbOpts []DBOption, readOpts []Option, impOpts []ImportOption) (*DB, error) {
	if len(paths) == 0 {
		return nil, errors.New("file2db: paths 不能为空")
	}

	// 先应用连接选项，才能知道要打开哪个驱动 / 哪个库
	dbCfg := defaultDBConfig()
	for _, o := range dbOpts {
		o(&dbCfg)
	}
	db, err := Open(dbCfg.DriveName, dbCfg.DSN, dbOpts...)
	if err != nil {
		return nil, err
	}

	results, err := Import(ctx, db, paths, readOpts, impOpts)
	if err != nil {
		db.Close() // 导入失败就不把半成品连接交出去
		return nil, err
	}

	// 配置了 Logger 才输出一行汇总，默认静默
	impCfg := defaultOptions()
	for _, o := range impOpts {
		o(&impCfg)
	}
	if impCfg.Logger != nil {
		var rows, skipped int64
		for _, r := range results {
			rows += r.Rows
			skipped += r.Skipped
		}
		impCfg.Logger.Printf("file2db: 导入完成，%d 个文件 / %d 行（跳过 %d 行）-> %s",
			len(results), rows, skipped, dbCfg.DSN)
	}
	return db, nil
}
