// Package logger is the framework logger: leveled, structured (log/slog) and
// configured by env vars. LOG_LEVEL: debug | info | success | error (default
// info). LOG_FORMAT: text | json (default text).
package logger

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"prea/common"
)

// Level order: Debug < Info < Success < Error.
const (
	LevelDebug   = slog.LevelDebug
	LevelInfo    = slog.LevelInfo
	LevelSuccess = slog.Level(2)
	LevelError   = slog.LevelError
)

// Log is the logger behind the package functions. It is also slog's default,
// so log.Printf and slog.Info from any library end up here.
var Log *slog.Logger

func init() {
	level, err := ParseLevel(common.GetEnv("LOG_LEVEL"))
	if err != nil {
		panic(err)
	}
	Log = New(os.Stdout, level, common.GetEnv("LOG_FORMAT") == "json")
	slog.SetDefault(Log)
}

// New builds a logger; use it in tests or to log somewhere else.
func New(w io.Writer, level slog.Level, json bool) *slog.Logger {
	opts := &slog.HandlerOptions{Level: level, ReplaceAttr: levelName}
	if json {
		return slog.New(slog.NewJSONHandler(w, opts))
	}
	return slog.New(slog.NewTextHandler(w, opts))
}

func ParseLevel(name string) (slog.Level, error) {
	switch strings.ToLower(name) {
	case "debug":
		return LevelDebug, nil
	case "", "info":
		return LevelInfo, nil
	case "success":
		return LevelSuccess, nil
	case "error":
		return LevelError, nil
	}
	return 0, fmt.Errorf("logger: invalid LOG_LEVEL %q (debug, info, success or error)", name)
}

func levelName(_ []string, a slog.Attr) slog.Attr {
	if a.Key == slog.LevelKey && a.Value.Any() == LevelSuccess {
		a.Value = slog.StringValue("SUCCESS")
	}
	return a
}

// Args are key/value pairs: logger.Info("user created", "id", u.ID).

func Debug(msg string, args ...any)   { Log.Debug(msg, args...) }
func Info(msg string, args ...any)    { Log.Info(msg, args...) }
func Success(msg string, args ...any) { Log.Log(context.Background(), LevelSuccess, msg, args...) }
func Error(msg string, args ...any)   { Log.Error(msg, args...) }
