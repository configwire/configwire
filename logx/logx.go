// Package logx is a tiny shared stderr logger for boot and pre-request
// diagnostics.
//
// Why manual stderr instead of the PocketBase app/request logs:
//   - Fresh installs disable PocketBase's persisted logs entirely
//     (Logs.MaxDays=0 pinned off-by-default in main.go), so anything only
//     written to the _logs collection is invisible to fresh-install
//     operators.
//   - Pre-request failures (IP blocks, admin denials, SDK-key 401s,
//     per-key 429s) are rejected in Router.BindFunc guards before a
//     handler runs, so they never reach PocketBase request logging
//     either. Writing these lines to stderr keeps them visible in
//     `docker logs` / journald / process output in exactly those cases.
//
// Prefix style matches the existing codebase: each line starts with
// "<component>: " (e.g. "configwire: ...", "ingest: ...") so logx output
// is greppable alongside the legacy log.Printf call sites.
//
// Privacy: logx never redacts — callers must only pass reason codes and
// status values. Never pass key material, tokens, emails, IPs, bucket
// values (?b=), attributes, or raw errors that may embed storage paths.
package logx

import (
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
)

// Level is a log severity. Higher values are more severe.
type Level int

const (
	// LevelDebug is the most verbose level.
	LevelDebug Level = iota
	// LevelInfo is the default level.
	LevelInfo
	// LevelWarn drops debug and info lines.
	LevelWarn
	// LevelError drops everything but errors.
	LevelError
)

// envVar is the single knob for verbosity.
const envVar = "CONFIGWIRE_LOG_LEVEL"

// defaultComponent is used when New is given an empty component and for
// the zero value, keeping lines greppable as "configwire: ...".
const defaultComponent = "configwire"

// std is the fallback stderr writer for zero-value Loggers.
var std = log.New(os.Stderr, "", log.LstdFlags)

// Logger is a prefixed stderr logger. The zero value is usable and
// behaves like New("configwire"). A nil *Logger is also usable. All
// methods are safe for concurrent use (the underlying log.Logger
// serializes writes; level checks read no mutable state).
type Logger struct {
	component string
	out       *log.Logger
	once      sync.Once
}

// New returns a Logger that prefixes every line with "<component>: ",
// matching the existing log.Printf("configwire: ...") / "ingest: ..."
// style. An empty component falls back to "configwire".
func New(component string) *Logger {
	if strings.TrimSpace(component) == "" {
		component = defaultComponent
	}
	return &Logger{
		component: component,
		out:       log.New(os.Stderr, "", log.LstdFlags),
	}
}

// initOut lazily installs the stderr writer for zero-value Loggers.
func (l *Logger) initOut() {
	l.once.Do(func() {
		if l.out == nil {
			l.out = log.New(os.Stderr, "", log.LstdFlags)
		}
		if strings.TrimSpace(l.component) == "" {
			l.component = defaultComponent
		}
	})
}

// currentLevel reads CONFIGWIRE_LOG_LEVEL on every call (debug/info/
// warn/error, case-insensitive, surrounding whitespace ignored).
// Unset or unrecognized values default to info so boot diagnostics stay
// visible without configuration.
func currentLevel() Level {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(envVar))) {
	case "debug":
		return LevelDebug
	case "warn", "warning":
		return LevelWarn
	case "error":
		return LevelError
	default:
		return LevelInfo
	}
}

// emit writes one "<component>: <msg>" line when lvl passes the gate.
// A nil receiver falls back to the package stderr writer.
func (l *Logger) emit(lvl Level, msg string) {
	if lvl < currentLevel() {
		return
	}
	if l == nil {
		std.Printf("%s: %s", defaultComponent, msg)
		return
	}
	l.initOut()
	l.out.Printf("%s: %s", l.component, msg)
}

// Debug logs a verbose diagnostic, hidden unless CONFIGWIRE_LOG_LEVEL=debug.
func (l *Logger) Debug(format string, args ...any) {
	l.emit(LevelDebug, fmt.Sprintf(format, args...))
}

// Info logs a normal operational line (default visibility).
func (l *Logger) Info(format string, args ...any) {
	l.emit(LevelInfo, fmt.Sprintf(format, args...))
}

// Warn logs a recoverable problem (rate limiting, saturation, fallbacks).
func (l *Logger) Warn(format string, args ...any) {
	l.emit(LevelWarn, fmt.Sprintf(format, args...))
}

// Error logs a failure that needs operator attention.
func (l *Logger) Error(format string, args ...any) {
	l.emit(LevelError, fmt.Sprintf(format, args...))
}

// Boot logs a startup diagnostic. Boot lines always emit regardless of
// CONFIGWIRE_LOG_LEVEL: a process that fails to start must say so even
// under a restrictive level.
func (l *Logger) Boot(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if l == nil {
		std.Printf("%s: %s", defaultComponent, msg)
		return
	}
	l.initOut()
	l.out.Printf("%s: %s", l.component, msg)
}

// Request logs a pre-request guard decision without PII: HTTP method,
// path, reason code, and status only. method/path/reason must already be
// free of key material, tokens, emails, IPs, buckets, and attributes
// (pass reason codes such as "missing-key" or "rate-limited", never
// header/query values). Status >= 400 emits at warn level, below that at
// info, so failures stay visible under CONFIGWIRE_LOG_LEVEL=warn.
func (l *Logger) Request(method, path, reason string, status int) {
	lvl := LevelInfo
	if status >= 400 {
		lvl = LevelWarn
	}
	l.emit(lvl, fmt.Sprintf("%s %s reason=%s status=%d", method, path, reason, status))
}
