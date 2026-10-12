// Package logger is the framework logger: leveled and structured (log/slog).
// It starts at info, in text; Configure changes it at startup.
package logger

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
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
	Configure("info", "text")
}

// Configure replaces Log (and slog's default) with one writing to stdout.
// level: debug | info | success | error. format: text | json.
func Configure(level, format string) error {
	l, err := ParseLevel(level)
	if err != nil {
		return err
	}
	if format != "" && format != "text" && format != "json" {
		return fmt.Errorf("logger: invalid format %q (text or json)", format)
	}
	Log = New(os.Stdout, l, format == "json")
	slog.SetDefault(Log)
	return nil
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
	return 0, fmt.Errorf("logger: invalid level %q (debug, info, success or error)", name)
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
