package file2db

import (
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/xuri/excelize/v2"
	"golang.org/x/text/encoding/simplifiedchinese"
)

// newTestLogger 返回一个不带到时间戳的 logger，输出写进 w，便于断言日志内容。
func newTestLogger(w io.Writer) *log.Logger {
	return log.New(w, "", 0)
}

// mustGBK 把文本编码成 GBK/GB18030 字节，模拟国内系统导出的 CSV。
func mustGBK(t *testing.T, s string) []byte {
	t.Helper()
	out, err := simplifiedchinese.GB18030.NewEncoder().String(s)
	if err != nil {
		t.Fatalf("GBK 编码失败: %v", err)
	}
	return []byte(out)
}

// newTestDB 打开一个临时文件库（每个测试一份，测试结束自动关闭并删掉）。
// 用文件库而不是 :memory:，这样 WAL / PRAGMA / 事务行为与生产一致。
func newTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open("sqlite", filepath.Join(t.TempDir(), "test.db"), WithMaxOpenConns(1))
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// countRows 统计表行数，表不存在直接让测试失败。
func countRows(t *testing.T, db Database, table string) int64 {
	t.Helper()
	n, err := CountRows(context.Background(), db, table)
	if err != nil {
		t.Fatalf("统计 %s 行数失败: %v", table, err)
	}
	return n
}

// tableSQL 返回建表语句（从 sqlite_master 里读），用来断言列类型等 DDL 细节。
func tableSQL(t *testing.T, db Database, table string) string {
	t.Helper()
	var sql string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name = ?`, table).Scan(&sql); err != nil {
		t.Fatalf("读取 %s 建表语句失败: %v", table, err)
	}
	return sql
}

// queryStrings 查单列并把结果按行拼成字符串切片，便于直接比较。
func queryStrings(t *testing.T, db Database, query string, args ...any) []string {
	t.Helper()
	rows, err := db.Query(query, args...)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var s any
		if err := rows.Scan(&s); err != nil {
			t.Fatalf("扫描失败: %v", err)
		}
		out = append(out, anyToString(s))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("遍历失败: %v", err)
	}
	return out
}

// writeFile 写一个测试文件并返回完整路径。
func writeFile(t *testing.T, name string, content []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("写测试文件 %s 失败: %v", name, err)
	}
	return path
}

// sheetSpec 描述要生成的 Excel 工作表。
type sheetSpec struct {
	name string
	rows [][]string
}

// writeXLSX 用 excelize 现场生成 xlsx（不往仓库里塞二进制样本）。
// 空字符串单元格不写入，这样才能造出 Excel 稀疏存储的「行尾缺列」场景。
func writeXLSX(t *testing.T, file string, sheets []sheetSpec) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), file)

	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	for i, sh := range sheets {
		if i == 0 {
			if sh.name != "Sheet1" {
				if err := f.SetSheetName("Sheet1", sh.name); err != nil {
					t.Fatalf("重命名首个 Sheet 失败: %v", err)
				}
			}
		} else {
			if _, err := f.NewSheet(sh.name); err != nil {
				t.Fatalf("新建 Sheet %q 失败: %v", sh.name, err)
			}
		}
		for r, row := range sh.rows {
			for c, v := range row {
				if v == "" {
					continue
				}
				cell, err := excelize.CoordinatesToCellName(c+1, r+1)
				if err != nil {
					t.Fatalf("计算单元格失败: %v", err)
				}
				if err := f.SetCellValue(sh.name, cell, v); err != nil {
					t.Fatalf("写单元格 %s!%s 失败: %v", sh.name, cell, err)
				}
			}
		}
	}
	if err := f.SaveAs(path); err != nil {
		t.Fatalf("保存 xlsx 失败: %v", err)
	}
	return path
}
