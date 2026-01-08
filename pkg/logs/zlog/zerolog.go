package zlog

import (
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// Trace/Tracef (跟踪级别)
// Debug/Debugf (调试级别)
// Info/Infof (信息级别)
// Warn/Warnf (警告级别)
// Error/Errorf (错误级别)
// Panic/Panicf (恐慌级别)
// Fatal/Fatalf (致命级别)
func init() {
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	consoleWriter := zerolog.ConsoleWriter{
		Out:        os.Stdout,
		TimeFormat: time.DateTime,
	}
	// logger := zerolog.New(zerolog.MultiLevelWriter(consoleWriter, fileWriter)).
	logger := zerolog.New(consoleWriter).
		With().
		Timestamp().
		Caller().
		Logger()
	log.Logger = logger
	zerolog.CallerSkipFrameCount = 3
}

func Info(msg string) {
	log.Info().Msg(msg)
}

func Infof(format string, args ...interface{}) {
	log.Info().Msgf(format, args...)
}
func Debug(msg string) {
	log.Debug().Msg(msg)
}

func Debugf(format string, args ...interface{}) {
	log.Debug().Msgf(format, args...)
}

func Warn(msg string) {
	log.Warn().Msg(msg)
}

func Warnf(format string, args ...interface{}) {
	log.Warn().Msgf(format, args...)
}

func Error(msg string) {
	log.Error().Msg(msg)
}

func Errorf(format string, args ...interface{}) {
	log.Error().Msgf(format, args...)
}

func Trace(msg string) {
	log.Trace().Msg(msg)
}

func Tracef(format string, args ...interface{}) {
	log.Trace().Msgf(format, args...)
}

func Panic(msg string) {
	log.Panic().Msg(msg)
}

func Panicf(format string, args ...interface{}) {
	log.Panic().Msgf(format, args...)
}

func Fatal(msg string) {
	log.Fatal().Msg(msg)
}

func Fatalf(format string, args ...interface{}) {
	log.Fatal().Msgf(format, args...)
}
