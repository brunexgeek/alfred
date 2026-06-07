package extra

import (
	"fmt"
	"log"
	"os"
	"strings"

	"golang.org/x/term"
)

type Level int

const (
	TraceLevel Level = iota
	DebugLevel
	InfoLevel
	WarnLevel
	ErrorLevel
)

const (
	reset  = "\033[0m"
	gray   = "\033[90m"
	blue   = "\033[34m"
	green  = "\033[32m"
	yellow = "\033[33m"
	red    = "\033[31m"
)

type Logger struct {
	level    Level
	logger   *log.Logger
	useColor bool
}

var defaultLevel Level = InfoLevel
var defaultLog *Logger = NewLogger()

func SetDefaultLevel(level Level) {
	if level >= TraceLevel && level <= ErrorLevel {
		defaultLevel = level
	} else {
		defaultLevel = InfoLevel
	}
	defaultLog.SetLevel(defaultLevel)
}

func GetDefaultLog() *Logger {
	return defaultLog
}

func NewLogger() *Logger {
	return &Logger{
		level:    defaultLevel,
		logger:   log.New(os.Stdout, "", log.LstdFlags|log.Lmicroseconds),
		useColor: term.IsTerminal(int(os.Stdout.Fd())),
	}
}

func ParseLevel(level string) Level {
	switch strings.ToLower(level) {
	case "trace":
		return TraceLevel
	case "debug":
		return DebugLevel
	case "info":
		return InfoLevel
	case "warn", "warning":
		return WarnLevel
	case "error":
		return ErrorLevel
	default:
		return InfoLevel
	}
}

func (l *Logger) SetLevel(level Level) {
	if level >= TraceLevel && level <= ErrorLevel {
		l.level = level
	}
}

func (l *Logger) logf(level Level, label string, color string, format string, args ...any) {
	if level < l.level {
		return
	}

	if l.useColor {
		label = color + label + reset
	}

	msg := fmt.Sprintf(format, args...)
	l.logger.Printf("[%s] %s", label, msg)
}

func (l *Logger) Tracef(format string, args ...any) {
	l.logf(TraceLevel, "TRACE", gray, format, args...)
}

func (l *Logger) Debugf(format string, args ...any) {
	l.logf(DebugLevel, "DEBUG", blue, format, args...)
}

func (l *Logger) Infof(format string, args ...any) {
	l.logf(InfoLevel, "INFO", green, format, args...)
}

func (l *Logger) Warnf(format string, args ...any) {
	l.logf(WarnLevel, "WARN", yellow, format, args...)
}

func (l Logger) Warn(err error) {
	l.logf(ErrorLevel, "WARN", yellow, "%s", err)
}

func (l *Logger) Errorf(format string, args ...any) {
	l.logf(ErrorLevel, "ERROR", red, format, args...)
}

func (l Logger) Error(err error) {
	l.logf(ErrorLevel, "ERROR", red, "%s", err)
}
