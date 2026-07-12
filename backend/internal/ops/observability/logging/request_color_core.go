package logging

import (
	"bytes"
	"fmt"
	"hash/fnv"
	"math"
	"os"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var requestColorPalette = []string{
	"\x1b[38;5;39m",
	"\x1b[38;5;45m",
	"\x1b[38;5;51m",
	"\x1b[38;5;82m",
	"\x1b[38;5;118m",
	"\x1b[38;5;220m",
	"\x1b[38;5;208m",
	"\x1b[38;5;203m",
	"\x1b[38;5;198m",
	"\x1b[38;5;201m",
	"\x1b[38;5;141m",
	"\x1b[38;5;111m",
}

type requestColorMode uint8

const (
	requestColorFallback requestColorMode = iota
	requestColor256
	requestColorTrue
)

type requestColorCore struct {
	encoder zapcore.Encoder
	output  zapcore.WriteSyncer
	level   zapcore.LevelEnabler
	color   string
}

func newRequestColorCore(encoder zapcore.Encoder, output zapcore.WriteSyncer, level zapcore.LevelEnabler) zapcore.Core {
	return &requestColorCore{encoder: encoder, output: output, level: level}
}

func (c *requestColorCore) Enabled(level zapcore.Level) bool {
	return c.level.Enabled(level)
}

func (c *requestColorCore) With(fields []zap.Field) zapcore.Core {
	clone := &requestColorCore{
		encoder: c.encoder.Clone(),
		output:  c.output,
		level:   c.level,
		color:   nonEmptyRequestColor(c.color, requestColorFromFields(fields)),
	}
	for _, field := range fields {
		field.AddTo(clone.encoder)
	}
	return clone
}

func (c *requestColorCore) Check(entry zapcore.Entry, checked *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if !c.Enabled(entry.Level) {
		return checked
	}
	return checked.AddCore(entry, c)
}

func (c *requestColorCore) Write(entry zapcore.Entry, fields []zap.Field) error {
	buffer, err := c.encoder.EncodeEntry(entry, fields)
	if err != nil {
		return err
	}
	defer buffer.Free()

	color := nonEmptyRequestColor(c.color, requestColorFromFields(fields))
	if color == "" {
		_, err = c.output.Write(buffer.Bytes())
		return err
	}

	raw := buffer.Bytes()
	hasNewline := bytes.HasSuffix(raw, []byte{'\n'})
	if hasNewline {
		raw = raw[:len(raw)-1]
	}
	colored := make([]byte, 0, len(color)+len(raw)+len(ansiReset)+1)
	colored = append(colored, color...)
	colored = append(colored, raw...)
	colored = append(colored, ansiReset...)
	if hasNewline {
		colored = append(colored, '\n')
	}
	_, err = c.output.Write(colored)
	return err
}

func (c *requestColorCore) Sync() error {
	return c.output.Sync()
}

func requestColorFromFields(fields []zap.Field) string {
	for _, field := range fields {
		if field.Key == "request_id" && field.Type == zapcore.StringType {
			return requestColor(field.String)
		}
	}
	return ""
}

func requestColor(requestID string) string {
	requestID = accessRequestID(requestID)
	if requestID == "-" || len(requestColorPalette) == 0 {
		return ""
	}
	hash := requestColorHash(requestID)
	switch detectRequestColorMode() {
	case requestColorTrue:
		red, green, blue := moderateRequestRGB(hash)
		return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", red, green, blue)
	case requestColor256:
		red, green, blue := moderateRequestRGB(hash)
		return fmt.Sprintf("\x1b[38;5;%dm", xtermCubeIndex(red, green, blue))
	default:
		return requestColorPalette[int(hash)%len(requestColorPalette)]
	}
}

func requestColorHash(requestID string) uint32 {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(requestID))
	return hash.Sum32()
}

func detectRequestColorMode() requestColorMode {
	colorTerm := strings.ToLower(strings.TrimSpace(os.Getenv("COLORTERM")))
	term := strings.ToLower(strings.TrimSpace(os.Getenv("TERM")))
	if strings.Contains(colorTerm, "truecolor") || strings.Contains(colorTerm, "24bit") ||
		strings.Contains(term, "truecolor") || strings.Contains(term, "direct") {
		return requestColorTrue
	}
	if strings.Contains(term, "256color") {
		return requestColor256
	}
	return requestColorFallback
}

func moderateRequestRGB(hash uint32) (int, int, int) {
	hue := float64(hash%360) / 360
	saturation := 0.42 + float64((hash>>9)%17)/100
	lightness := 0.56 + float64((hash>>17)%9)/100
	return hslToRGB(hue, saturation, lightness)
}

func hslToRGB(hue float64, saturation float64, lightness float64) (int, int, int) {
	if saturation == 0 {
		value := int(math.Round(lightness * 255))
		return value, value, value
	}
	q := lightness * (1 + saturation)
	if lightness >= 0.5 {
		q = lightness + saturation - lightness*saturation
	}
	p := 2*lightness - q
	channel := func(offset float64) int {
		value := hue + offset
		if value < 0 {
			value++
		}
		if value > 1 {
			value--
		}
		switch {
		case value < 1.0/6:
			value = p + (q-p)*6*value
		case value < 0.5:
			value = q
		case value < 2.0/3:
			value = p + (q-p)*(2.0/3-value)*6
		default:
			value = p
		}
		return int(math.Round(value * 255))
	}
	return channel(1.0 / 3), channel(0), channel(-1.0 / 3)
}

func xtermCubeIndex(red int, green int, blue int) int {
	return 16 + 36*nearestModerateCubeLevel(red) + 6*nearestModerateCubeLevel(green) + nearestModerateCubeLevel(blue)
}

func nearestModerateCubeLevel(value int) int {
	levels := [...]int{95, 135, 175, 215}
	bestIndex := 0
	bestDistance := math.MaxInt
	for index, level := range levels {
		distance := value - level
		if distance < 0 {
			distance = -distance
		}
		if distance < bestDistance {
			bestDistance = distance
			bestIndex = index
		}
	}
	return bestIndex + 1
}

func nonEmptyRequestColor(primary string, fallback string) string {
	if primary != "" {
		return primary
	}
	return fallback
}
