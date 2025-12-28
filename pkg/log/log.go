package log

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	// "github.com/labstack/gommon/log"
)

// func init() {
// 	// 打开/创建日志文件（追加模式，不存在则创建）
// 	file, err := os.OpenFile("app.log", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644) // 文件权限：所有者读写，其他只读
// 	if err != nil {
// 		fmt.Println("打开日志文件失败：%w", err)
// 	}
// 	// defer file.Close()
// 	log.SetLevel(log.DEBUG)
// 	log.SetOutputFileAndStdout(file)
// }

// func main() {
// 	defer log.Close()
// 	log.Info("hello world")
// 	log.Fatal("hello world")
// 	log.Debugf("hello world%s", "123")
// }

type Lvl uint8

const (
	TRACE Lvl = iota + 1
	DEBUG
	INFO
	WARN
	ERROR
	OFF
	panicLevel
	fatalLevel
)

// 定义颜色常量，聚焦debug级别（蓝色）
const (
	// DebugColor debug级别日志颜色（蓝色前景）
	DebugColor = "\x1b[34m"
	// ResetColor 重置样式
	ResetColor = "\x1b[0m"
	// 其他级别颜色（可选，用于对比）
	InfoColor  = "\x1b[32m"
	WarnColor  = "\x1b[33m"
	ErrorColor = "\x1b[31m"
)

func init() {

}

var Logger = &logger{Logger: log.Default(), level: INFO}

type logger struct {
	Logger *log.Logger
	level  Lvl
	File   *os.File
}

func SetLevel(level Lvl) {
	Logger.level = level
}
func Level() Lvl {
	return Logger.level
}
func SetOutput(w io.Writer) {
	Logger.Logger.SetOutput(w)
}
func SetOutputFile(file *os.File) {
	Logger.Logger.SetOutput(file)
}
func SetOutputFileAndStdout(file *os.File) {
	Logger.File = file
	SetOutput(io.MultiWriter(os.Stdout, file))
}
func Close() {
	if Logger.File != nil {
		Logger.File.Close()
	}
}
func Info(message ...interface{}) {
	if Logger.level <= INFO {
		_, file, line, _ := runtime.Caller(1)
		Logger.Logger.Output(3, fmt.Sprintf("%s:%d %s%s%s %v", filepath.Base(file), line, InfoColor, "INF", ResetColor, fmt.Sprint(message...)))
	}
}
func Infof(format string, args ...interface{}) {
	if Logger.level <= INFO {
		_, file, line, _ := runtime.Caller(1)
		Logger.Logger.Output(3, fmt.Sprintf("%s:%d %s%s%s %v", filepath.Base(file), line, InfoColor, "INF", ResetColor, fmt.Sprintf(format, args...)))
	}
}
func Debug(message ...interface{}) {
	if Logger.level <= DEBUG {
		_, file, line, _ := runtime.Caller(1)
		Logger.Logger.Output(3, fmt.Sprintf("%s:%d %s%s%s %v", filepath.Base(file), line, DebugColor, "DEB", ResetColor, fmt.Sprint(message...)))
	}
}

func Debugf(format string, args ...interface{}) {
	if Logger.level <= DEBUG {
		_, file, line, _ := runtime.Caller(1)
		Logger.Logger.Output(3, fmt.Sprintf("%s:%d %s%s%s %v", filepath.Base(file), line, DebugColor, "DEB", ResetColor, fmt.Sprintf(format, args...)))
	}
}

func Warn(message ...interface{}) {
	if Logger.level <= WARN {
		_, file, line, _ := runtime.Caller(1)
		Logger.Logger.Output(3, fmt.Sprintf("%s:%d %s%s%s %v", filepath.Base(file), line, DebugColor, "WAR", ResetColor, fmt.Sprint(message...)))
	}
}

func Warnf(format string, args ...interface{}) {
	if Logger.level <= WARN {
		_, file, line, _ := runtime.Caller(1)
		Logger.Logger.Output(3, fmt.Sprintf("%s:%d %s%s%s %v", filepath.Base(file), line, WarnColor, "WAR", ResetColor, fmt.Sprintf(format, args...)))
	}
}

func Error(message ...interface{}) {
	if Logger.level <= ERROR {
		_, file, line, _ := runtime.Caller(1)
		Logger.Logger.Output(3, fmt.Sprintf("%s:%d %s%s%s %v", filepath.Base(file), line, DebugColor, "ERR", ResetColor, fmt.Sprint(message...)))
	}
}

func Errorf(format string, args ...interface{}) {
	if Logger.level <= ERROR {
		_, file, line, _ := runtime.Caller(1)
		Logger.Logger.Output(3, fmt.Sprintf("%s:%d %s%s%s %v", filepath.Base(file), line, ErrorColor, "ERR", ResetColor, fmt.Sprintf(format, args...)))
	}
}

func Trace(message ...interface{}) {
	if Logger.level <= TRACE {
		_, file, line, _ := runtime.Caller(1)
		Logger.Logger.Output(3, fmt.Sprintf("%s:%d %s%s%s %v", filepath.Base(file), line, DebugColor, "TRACE", ResetColor, fmt.Sprint(message...)))
	}
}

func Tracef(format string, args ...interface{}) {
	if Logger.level <= TRACE {
		_, file, line, _ := runtime.Caller(1)
		Logger.Logger.Output(3, fmt.Sprintf("%s:%d %s%s%s %v", filepath.Base(file), line, ErrorColor, "TRACE", ResetColor, fmt.Sprintf(format, args...)))
	}
}

func Panic(message ...interface{}) {
	msg := fmt.Sprint(message...)
	_, file, line, _ := runtime.Caller(1)
	Logger.Logger.Output(3, fmt.Sprintf("%s:%d %s%s%s %v", filepath.Base(file), line, ErrorColor, "Panic", ResetColor, msg))
	panic(msg)
}

func Panicf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	_, file, line, _ := runtime.Caller(1)
	Logger.Logger.Output(3, fmt.Sprintf("%s:%d %s%s%s %v", filepath.Base(file), line, ErrorColor, "Panic", ResetColor, msg))
	panic(msg)
}

func Fatal(message ...interface{}) {
	_, file, line, _ := runtime.Caller(1)
	Logger.Logger.Output(3, fmt.Sprintf("%s:%d %s%s%s %v", filepath.Base(file), line, ErrorColor, "Fatal", ResetColor, fmt.Sprint(message...)))
	os.Exit(1)
}

func Fatalf(format string, args ...interface{}) {
	_, file, line, _ := runtime.Caller(1)
	Logger.Logger.Output(3, fmt.Sprintf("%s:%d %s%s%s %v", filepath.Base(file), line, ErrorColor, "Fatal", ResetColor, fmt.Sprintf(format, args...)))
	os.Exit(1)
}
