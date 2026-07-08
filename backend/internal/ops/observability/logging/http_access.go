package logging

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/mattn/go-isatty"
)

const (
	httpAccessMessage = "http request completed"

	ansiReset     = "\x1b[0m"
	ansiFgBlack   = "\x1b[30m"
	ansiFgWhite   = "\x1b[97m"
	ansiFgBlue    = "\x1b[34m"
	ansiFgGreen   = "\x1b[32m"
	ansiFgYellow  = "\x1b[33m"
	ansiFgRed     = "\x1b[31m"
	ansiFgCyan    = "\x1b[36m"
	ansiFgMagenta = "\x1b[35m"
	ansiFgHiBlack = "\x1b[90m"
	ansiBgGreen   = "\x1b[42m"
	ansiBgYellow  = "\x1b[43m"
	ansiBgRed     = "\x1b[41m"
	ansiBgMagenta = "\x1b[45m"
)

type HTTPAccessEntry struct {
	Timestamp time.Time
	RequestID string
	Kind      string
	Method    string
	Path      string
	Status    int
	Duration  time.Duration
	Remote    string
}

type HTTPAccessFormatter struct {
	writer   io.Writer
	enabled  bool
	useColor bool
}

func NewHTTPAccessFormatter(enabled bool) HTTPAccessFormatter {
	fd := os.Stdout.Fd()
	return HTTPAccessFormatter{
		enabled:  enabled,
		writer:   os.Stdout,
		useColor: isatty.IsTerminal(fd) || isatty.IsCygwinTerminal(fd),
	}
}

func (f HTTPAccessFormatter) WriteSummary(entry HTTPAccessEntry) error {
	if !f.enabled || f.writer == nil {
		return nil
	}
	_, err := fmt.Fprintln(f.writer, f.FormatSummary(entry))
	return err
}

func (f HTTPAccessFormatter) FormatSummary(entry HTTPAccessEntry) string {
	timestamp := f.muted(formatAccessTimestamp(entry.Timestamp))
	requestID := f.requestIDBlock(entry.RequestID)
	kind := f.kindBlock(entry.Kind)
	status := f.statusBlock(entry.Status)
	method := f.methodBlock(strings.ToUpper(strings.TrimSpace(entry.Method)))
	path := f.pathBlock(entry.Path)
	duration := padLeft(formatDurationMillis(entry.Duration), 8)
	duration = f.paintDuration(duration, entry.Duration)

	parts := []string{
		timestamp,
		requestID,
		kind,
		status,
		method,
		path,
		duration,
	}
	remote := strings.TrimSpace(entry.Remote)
	if remote != "" {
		parts = append(parts, f.muted(remote))
	}
	return strings.Join(parts, " ")
}

func (f HTTPAccessFormatter) requestIDBlock(requestID string) string {
	value := accessRequestID(requestID)
	if value == "-" {
		return f.colorize("["+value+"]", ansiFgRed)
	}
	return f.muted("[" + value + "]")
}

func (f HTTPAccessFormatter) kindBlock(kind string) string {
	kind = strings.ToUpper(strings.TrimSpace(kind))
	if kind == "" {
		kind = "HTTP"
	}
	kind = padRight(kind, 4)
	switch strings.TrimSpace(kind) {
	case "WS":
		return f.colorize(kind, ansiFgMagenta)
	case "SSE":
		return f.colorize(kind, ansiFgCyan)
	case "POLL":
		return f.colorize(kind, ansiFgYellow)
	default:
		return f.muted(kind)
	}
}

func (f HTTPAccessFormatter) statusBlock(status int) string {
	text := " " + padLeft(strconv.Itoa(status), 3) + " "
	switch {
	case status >= 500:
		return f.colorize(text, ansiBgRed, ansiFgWhite)
	case status >= 400:
		return f.colorize(text, ansiBgYellow, ansiFgBlack)
	case status >= 300:
		return f.colorize(text, ansiBgMagenta, ansiFgWhite)
	default:
		return f.colorize(text, ansiBgGreen, ansiFgBlack)
	}
}

func (f HTTPAccessFormatter) methodBlock(method string) string {
	method = padRight(strings.TrimSpace(method), 7)
	switch strings.TrimSpace(method) {
	case http.MethodGet:
		return f.colorize(method, ansiFgBlue)
	case http.MethodPost:
		return f.colorize(method, ansiFgCyan)
	case http.MethodPut, http.MethodPatch:
		return f.colorize(method, ansiFgYellow)
	case http.MethodDelete:
		return f.colorize(method, ansiFgRed)
	default:
		return f.colorize(method, ansiFgMagenta)
	}
}

func (f HTTPAccessFormatter) pathBlock(path string) string {
	return padRight(ellipsize(trimOrFallback(path, "/"), 52), 52)
}

func (f HTTPAccessFormatter) paintDuration(value string, d time.Duration) string {
	switch {
	case d >= 3*time.Second:
		return f.colorize(value, ansiFgRed)
	case d >= time.Second:
		return f.colorize(value, ansiFgYellow)
	case d >= 300*time.Millisecond:
		return f.colorize(value, ansiFgCyan)
	default:
		return f.colorize(value, ansiFgGreen)
	}
}

func (f HTTPAccessFormatter) muted(value string) string {
	return f.colorize(value, ansiFgHiBlack)
}

func (f HTTPAccessFormatter) colorize(value string, codes ...string) string {
	if !f.useColor || len(codes) == 0 {
		return value
	}
	return strings.Join(codes, "") + value + ansiReset
}

func formatDurationMillis(d time.Duration) string {
	ms := float64(d) / float64(time.Millisecond)
	switch {
	case ms >= 100:
		return fmt.Sprintf("%.0fms", ms)
	case ms >= 10:
		return fmt.Sprintf("%.1fms", ms)
	default:
		return fmt.Sprintf("%.2fms", ms)
	}
}

func formatAccessTimestamp(ts time.Time) string {
	if ts.IsZero() {
		ts = time.Now()
	}
	return ts.Format("2006-01-02T15:04:05.000Z07:00")
}

func accessRequestID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "-"
	}
	return value
}

func ellipsize(value string, width int) string {
	runes := []rune(value)
	if len(runes) <= width {
		return value
	}
	if width <= 1 {
		return string(runes[:width])
	}
	return string(runes[:width-1]) + "…"
}

func padRight(value string, width int) string {
	runes := []rune(value)
	if len(runes) >= width {
		return string(runes[:width])
	}
	return value + strings.Repeat(" ", width-len(runes))
}

func padLeft(value string, width int) string {
	runes := []rune(value)
	if len(runes) >= width {
		return value
	}
	return strings.Repeat(" ", width-len(runes)) + value
}

func trimOrFallback(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func HTTPAccessMessage() string {
	return httpAccessMessage
}

func HTTPStatusFromRecorder(status int) int {
	if status == 0 {
		return http.StatusOK
	}
	return status
}
