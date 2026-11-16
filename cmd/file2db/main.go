// file2db-cli 是 file2db 包的命令行工具：Excel / CSV <-> 数据库（默认 SQLite）。
//
// 用法：
//
//	file2db-cli <命令> [参数...]              # import / export / query / tables / version / help
//	file2db-cli -config 任务.json             # 不给命令时按配置顺序跑：import -> queries -> export
//
// 参数的取值优先级是「内置默认值 < 配置文件 < 命令行」，所以参数很多时
// 用配置文件描述整套流程最省事，命令行只用来临时改几项（详见 flags.go 的 parseArgs）。
//
// 长任务可以随时 Ctrl+C 中断：中断会让正在写的那一批事务回滚、已提交的批次保留，
// 导出则删掉半截产物；也可以用 -timeout 给整次执行设一个总时限（适合定时任务）。
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/chengcong1/forky/pkg/file2db"

	_ "github.com/go-sql-driver/mysql" // 驱动名 "mysql"
	_ "github.com/lib/pq"              // 驱动名 "postgres"
)

func main() {
	os.Exit(run())
}

// run 是真正的入口：它返回退出码而不是直接 os.Exit，
// 这样 defer（关库、恢复信号处理）一定会执行 —— 否则 SQLite 的 WAL 可能来不及落盘。
func run() int {
	inv, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "错误："+err.Error())
		fmt.Fprintln(os.Stderr, "用法：file2db-cli <命令> [参数...]；执行 file2db-cli help 查看全部参数")
		return 2
	}

	switch inv.cmd {
	case "help":
		printUsage(os.Stdout, inv.fs)
		return 0
	case "version":
		fmt.Printf("file2db-cli %s\n", version)
		return 0
	}

	ctx, stop, err := newContext(inv.cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "错误："+err.Error())
		return 2
	}
	defer stop()

	db, err := openDB(inv.cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "错误："+err.Error())
		return 1
	}
	defer db.Close()

	if err := execute(ctx, inv, db); err != nil {
		return reportError(inv.cfg, err)
	}
	return 0
}

// newContext 把「Ctrl+C / SIGTERM」和配置里的 timeout 合成一个可取消的 context。
//
// 信号必须先接管再干活：库里的导入 / 导出都会检查 ctx，所以中断能在
// 「一行 / 一批」的边界上停下来，而不是把进程直接打死留下半个事务。
//
// 中断是两级的：第一次只取消（优雅收尾），第二次强制退出 ——
// 万一卡在驱动内部的不可中断调用上，用户还有办法脱身。
func newContext(cfg *Config) (context.Context, context.CancelFunc, error) {
	timeout, err := parseTimeout(cfg.Timeout)
	if err != nil {
		return nil, nil, err
	}

	var (
		ctx    context.Context
		cancel context.CancelFunc
	)
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), timeout)
	} else {
		ctx, cancel = context.WithCancel(context.Background())
	}

	sig := make(chan os.Signal, 2)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sig // 第一次：优雅取消
		cancel()
		<-sig // 第二次：不再等待
		fmt.Fprintln(os.Stderr, "再次收到中断信号，强制退出")
		os.Exit(130)
	}()

	stop := func() {
		signal.Stop(sig)
		cancel()
	}
	return ctx, stop, nil
}

// parseTimeout 解析配置里的超时字符串：空 / "0" 表示不超时。
func parseTimeout(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "0" {
		return 0, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("timeout 解析失败 %q：请写成 30m / 2h 这种形式，0 表示不超时", s)
	}
	if d < 0 {
		return 0, fmt.Errorf("timeout 不能为负数：%q", s)
	}
	return d, nil
}

// reportError 打印错误并返回进程退出码：
// 被中断（Ctrl+C）是 130、超时是 124，其余按失败处理为 1。
func reportError(cfg *Config, err error) int {
	switch {
	case errors.Is(err, context.Canceled):
		fmt.Fprintln(os.Stderr, "已中断：未提交的数据已回滚，未写完的导出文件已删除")
		return 130
	case errors.Is(err, context.DeadlineExceeded):
		fmt.Fprintf(os.Stderr, "错误：超过超时时间 %s，任务已中止（未提交的数据已回滚、未写完的导出文件已删除）\n", cfg.Timeout)
		return 124
	default:
		fmt.Fprintln(os.Stderr, "错误："+err.Error())
		return 1
	}
}

// execute 按命令执行。
//
// 没有子命令时按「导入文件 -> 执行 queries -> 导出表」的顺序跑一遍，
// 这样一个配置文件就能描述整套动作（拉数、加工、导出的固定套路）。
func execute(ctx context.Context, inv *invocation, db *file2db.DB) error {
	cfg := inv.cfg
	switch inv.cmd {
	case "import":
		return runImport(ctx, cfg, db)
	case "export":
		return runExport(ctx, cfg, db)
	case "query":
		return runQueries(ctx, cfg, db)
	case "tables":
		return runTables(ctx, cfg, db)
	}

	// 没有子命令：配置里什么都没有就打印帮助，避免「什么都没发生」的困惑
	if len(cfg.Import.Files) == 0 && len(cfg.Queries) == 0 && !cfg.cli.sqlProvided && !hasExportTask(cfg) {
		printUsage(os.Stdout, inv.fs)
		return nil
	}
	if len(cfg.Import.Files) > 0 {
		if err := runImport(ctx, cfg, db); err != nil {
			return err
		}
	}
	if len(cfg.Queries) > 0 || cfg.cli.sqlProvided {
		if err := runQueries(ctx, cfg, db); err != nil {
			return err
		}
	}
	if hasExportTask(cfg) {
		if err := runExport(ctx, cfg, db); err != nil {
			return err
		}
	}
	return nil
}

// hasExportTask 判断这次调用有没有导出任务。
func hasExportTask(cfg *Config) bool {
	return cfg.Export.Dir != "" || cfg.Export.SingleFile != "" || len(cfg.Export.Tables) > 0
}
