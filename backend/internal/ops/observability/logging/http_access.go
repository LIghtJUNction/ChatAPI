package logging

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
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
	Phase     string
	RequestID string
	Kind      string
	Method    string
	Path      string
	Status    int
	Duration  time.Duration
	Remote    string
}

type HTTPAccessFormatter struct {
	writer        io.Writer
	enabled       bool
	useColor      bool
	terminalWidth func() int
}

func NewHTTPAccessFormatter(enabled bool) HTTPAccessFormatter {
	fd := os.Stdout.Fd()
	return HTTPAccessFormatter{
		enabled:       enabled,
		writer:        os.Stdout,
		useColor:      isatty.IsTerminal(fd) || isatty.IsCygwinTerminal(fd),
		terminalWidth: func() int { return stdoutTerminalWidth(int(fd)) },
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
	width := 0
	if f.terminalWidth != nil {
		width = f.terminalWidth()
	}
	return f.formatSummaryWidth(entry, width)
}

func (f HTTPAccessFormatter) formatSummaryWidth(entry HTTPAccessEntry, terminalWidth int) string {
	requestIDValue, pathValue, pathWidth, remoteValue, timestampValue := summaryValues(entry, terminalWidth)
	timestamp := f.muted(timestampValue)
	requestID := f.requestIDBlock(requestIDValue, entry.RequestID)
	kind := f.kindBlock(entry.Kind)
	status := f.statusBlock(entry.Status, entry.Phase)
	method := f.methodBlock(strings.ToUpper(strings.TrimSpace(entry.Method)))
	path := f.pathBlock(pathValue, pathWidth)
	duration := f.durationBlock(entry)

	parts := []string{
		timestamp,
		requestID,
		kind,
		status,
		method,
		path,
		duration,
	}
	remote := strings.TrimSpace(remoteValue)
	if remote != "" {
		parts = append(parts, f.muted(remote))
	}
	return strings.Join(parts, " ")
}

func (f HTTPAccessFormatter) requestIDBlock(displayRequestID string, colorRequestID string) string {
	value := accessRequestID(displayRequestID)
	if value == "-" {
		return f.colorize("["+value+"]", ansiFgRed)
	}
	return f.colorize("["+value+"]", requestColor(colorRequestID))
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

func (f HTTPAccessFormatter) statusBlock(status int, phase string) string {
	if phase == "start" {
		return f.muted(" ... ")
	}
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

func (f HTTPAccessFormatter) pathBlock(path string, width int) string {
	return padRight(compactRequestTarget(trimOrFallback(path, "/"), width), width)
}

func (f HTTPAccessFormatter) durationBlock(entry HTTPAccessEntry) string {
	if entry.Phase == "start" {
		return f.colorize(padLeft("START", 8), ansiFgCyan)
	}
	duration := padLeft(formatDurationMillis(entry.Duration), 8)
	return f.paintDuration(duration, entry.Duration)
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

func summaryValues(entry HTTPAccessEntry, terminalWidth int) (requestID string, path string, pathWidth int, remote string, timestamp string) {
	requestID = accessRequestID(entry.RequestID)
	path = requestSummaryPath(entry.Path)
	pathWidth = 52
	remote = strings.TrimSpace(entry.Remote)
	timestamp = formatAccessTimestamp(entry.Timestamp)
	if terminalWidth <= 0 {
		return requestID, path, pathWidth, remote, timestamp
	}

	const fixedWidth = 4 + 5 + 7 + 52 + 8 + 6 // kind, status, method, path, duration, separators
	lineWidth := len([]rune(timestamp)) + len([]rune(requestID)) + 2 + fixedWidth
	if remote != "" {
		lineWidth += 1 + len([]rune(remote))
	}
	if lineWidth <= terminalWidth {
		return requestID, path, pathWidth, remote, timestamp
	}

	overflow := lineWidth - terminalWidth
	requestRunes := len([]rune(requestID))
	requestBudget := maxInt(12, requestRunes-overflow)
	if requestBudget < requestRunes {
		requestID = ellipsizeMiddle(requestID, requestBudget)
		overflow -= requestRunes - requestBudget
	}
	if overflow > 0 {
		pathWidth = maxInt(18, 52-overflow)
		path = compactRequestTarget(path, pathWidth)
		overflow -= 52 - pathWidth
	}
	if overflow > 0 && remote != "" {
		overflow -= len([]rune(remote)) + 1
		remote = ""
	}
	if overflow > 0 {
		accessTime := entry.Timestamp
		if accessTime.IsZero() {
			accessTime = time.Now()
		}
		timestamp = accessTime.Format("15:04:05.000")
	}
	return requestID, path, pathWidth, remote, timestamp
}

func requestSummaryPath(value string) string {
	value = trimOrFallback(value, "/")
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.RawQuery == "" {
		return value
	}
	query := parsed.Query()
	for key := range query {
		if sensitiveQueryKey(key) {
			query[key] = []string{"[redacted]"}
		}
	}
	parsed.RawQuery = query.Encode()
	return parsed.RequestURI()
}

func sensitiveQueryKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	for _, fragment := range []string{"token", "secret", "password", "signature", "code", "key", "state"} {
		if strings.Contains(key, fragment) {
			return true
		}
	}
	return false
}

func compactRequestTarget(value string, width int) string {
	if len([]rune(value)) <= width {
		return value
	}
	path, query, found := strings.Cut(value, "?")
	if found {
		pairs := strings.Split(query, "&")
		fixedWidth := len([]rune(path)) + 1 + maxInt(0, len(pairs)-1)
		values := make([]string, len(pairs))
		keys := make([]string, len(pairs))
		compactValueCount := 0
		for index, pair := range pairs {
			key, rawValue, hasValue := strings.Cut(pair, "=")
			keys[index] = key
			fixedWidth += len([]rune(key))
			if hasValue {
				fixedWidth++
				values[index], _ = url.QueryUnescape(rawValue)
				if values[index] == "[redacted]" {
					fixedWidth += len([]rune(values[index]))
				} else {
					compactValueCount++
				}
			}
		}
		valueBudget := width - fixedWidth
		if valueBudget >= compactValueCount {
			remainingValues := compactValueCount
			for index := range pairs {
				if values[index] == "[redacted]" {
					pairs[index] = keys[index] + "=" + values[index]
					continue
				}
				if remainingValues == 0 {
					continue
				}
				budget := maxInt(1, valueBudget/remainingValues)
				compactValue := ellipsizeMiddle(values[index], budget)
				pairs[index] = keys[index] + "=" + compactValue
				valueBudget -= len([]rune(compactValue))
				remainingValues--
			}
			candidate := path + "?" + strings.Join(pairs, "&")
			if len([]rune(candidate)) <= width {
				return candidate
			}
		}
	}
	segments := strings.Split(value, "/")
	for index, segment := range segments {
		if len([]rune(segment)) > 20 {
			segments[index] = ellipsizeMiddle(segment, 14)
		}
	}
	return ellipsizeMiddle(strings.Join(segments, "/"), width)
}

func ellipsizeMiddle(value string, width int) string {
	runes := []rune(value)
	if len(runes) <= width {
		return value
	}
	if width <= 1 {
		return "…"
	}
	left := (width - 1) / 2
	right := width - left - 1
	return string(runes[:left]) + "…" + string(runes[len(runes)-right:])
}

func maxInt(left int, right int) int {
	if left > right {
		return left
	}
	return right
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
