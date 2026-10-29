package config

import (
	"fmt"
	"log/slog"
	"strings"
)

// ParseLogLevel converts a LOG_LEVEL value into a slog.Level. The empty string
// means info. Matching is case-insensitive and surrounding whitespace is
// ignored. Unknown values return an error so the caller can warn and fall back.
func ParseLogLevel(raw string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("config: unknown log level %q", raw)
	}
}
