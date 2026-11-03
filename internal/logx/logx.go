// Package logx builds the JSON slog loggers shared by ois, oic and oi.
package logx

import (
	"fmt"
	"io"
	"log/slog"
)

// ParseLevel accepts debug, info, warn and error; empty means info.
func ParseLevel(s string) (slog.Level, error) {
	var l slog.Level
	if s == "" {
		return slog.LevelInfo, nil
	}
	if err := l.UnmarshalText([]byte(s)); err != nil {
		return 0, fmt.Errorf("log_level %q: use debug, info, warn or error", s)
	}
	return l, nil
}

// New returns a JSON logger writing to w at level (info when invalid).
func New(w io.Writer, level string) *slog.Logger {
	l, _ := ParseLevel(level)
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: l}))
}
