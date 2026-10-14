package ui

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// Money renders a dollar amount with just enough precision to stay meaningful
// at the fractions of a cent that per-request spend produces.
func Money(v float64) string {
	abs := math.Abs(v)
	switch {
	case abs == 0:
		return "$0.00"
	case abs < 0.0001:
		// A single cheap request costs a handful of micro-dollars; rounding
		// those to $0.0000 would report the spend as free.
		return fmt.Sprintf("$%.6f", v)
	case abs < 0.01:
		return fmt.Sprintf("$%.4f", v)
	case abs < 1:
		return fmt.Sprintf("$%.3f", v)
	default:
		return fmt.Sprintf("$%.2f", v)
	}
}

// MoneyFixed renders a dollar amount with two decimals.
func MoneyFixed(v float64) string { return fmt.Sprintf("$%.2f", v) }

// plural renders a count with its noun, singular when the count is one.
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return Count(n) + " " + noun + "s"
}

// Count renders a thousands-separated integer.
func Count(n int) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	negative := strings.HasPrefix(s, "-")
	if negative {
		s = s[1:]
	}
	var parts []string
	for len(s) > 3 {
		parts = append([]string{s[len(s)-3:]}, parts...)
		s = s[:len(s)-3]
	}
	parts = append([]string{s}, parts...)
	out := strings.Join(parts, ",")
	if negative {
		return "-" + out
	}
	return out
}

// Share is spend as a fraction of limit, clamped to [0,1]. A non-positive
// limit means unlimited, which reports 0.
func Share(spend, limit float64) float64 {
	if limit <= 0 {
		return 0
	}
	return math.Max(0, math.Min(1, spend/limit))
}

// PercentText formats a fraction for display, keeping precision for the tiny
// fractions that early-period spend produces.
func PercentText(spend, limit float64) string {
	if limit <= 0 {
		return "  n/a"
	}
	pct := spend / limit * 100
	switch {
	case pct == 0:
		return "0.00%"
	case pct < 0.01:
		return fmt.Sprintf("%.4f%%", pct)
	case pct < 10:
		return fmt.Sprintf("%.2f%%", pct)
	default:
		return fmt.Sprintf("%.1f%%", pct)
	}
}

// HumanDuration renders a countdown at a glance: days when they matter,
// seconds when the reset is imminent.
func HumanDuration(d time.Duration) string {
	if d <= 0 {
		return "now"
	}
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60
	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh %02dm", hours, minutes)
	case minutes > 0:
		return fmt.Sprintf("%dm %02ds", minutes, seconds)
	default:
		return fmt.Sprintf("%ds", seconds)
	}
}

// Ago renders how long ago something happened, at sub-second resolution for
// the first minute so refreshes visibly land.
func Ago(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Second:
		return fmt.Sprintf("%dms ago", d.Milliseconds())
	case d < time.Minute:
		return fmt.Sprintf("%.1fs ago", d.Seconds())
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
}

// Date renders a reset timestamp as a short calendar date.
func Date(t time.Time) string { return t.Local().Format("2 Jan 2006") }

// In renders a future duration, collapsing to "now" once it has passed.
func In(d time.Duration) string {
	if d <= 0 {
		return "now"
	}
	return "in " + HumanDuration(d)
}

// DateClock renders a timestamp with the time of day.
func DateClock(t time.Time) string { return t.Local().Format("2 Jan 2006 15:04") }

// Sparkline draws a series as block glyphs, newest on the right.
func Sparkline(values []float64, width int) string {
	glyphs := []rune("▁▂▃▄▅▆▇█")
	if width <= 0 || len(values) == 0 {
		return ""
	}
	if len(values) > width {
		values = values[len(values)-width:]
	}
	lo, hi := values[0], values[0]
	for _, v := range values {
		lo = math.Min(lo, v)
		hi = math.Max(hi, v)
	}
	span := hi - lo
	var b strings.Builder
	for _, v := range values {
		if span <= 0 {
			b.WriteRune(glyphs[0])
			continue
		}
		idx := int((v - lo) / span * float64(len(glyphs)-1))
		if idx < 0 {
			idx = 0
		}
		if idx >= len(glyphs) {
			idx = len(glyphs) - 1
		}
		b.WriteRune(glyphs[idx])
	}
	return b.String()
}

// barGlyphs are the partial block characters, indexed by eighths.
var barGlyphs = []rune(" ▏▎▍▌▋▊▉█")

// Bar renders a proportional bar of exactly width cells, splitting the last
// cell into eighths so small fractions are still visible. Any non-zero
// fraction fills part of the first cell, because a bar that reads as empty
// next to a non-zero percentage is misleading. Colour is chosen by the caller
// from the returned fraction.
func Bar(fraction float64, width int) string {
	if width <= 0 {
		return ""
	}
	fraction = math.Max(0, math.Min(1, fraction))
	cells := fraction * float64(width)
	whole := int(cells)
	frac := cells - float64(whole)

	var b strings.Builder
	if whole == 0 && fraction > 0 {
		b.WriteRune(barGlyphs[1])
		whole = 1
	} else {
		if whole > 0 {
			b.WriteString(strings.Repeat("█", whole))
		}
		if whole < width {
			if idx := int(frac * 8); idx > 0 {
				b.WriteRune(barGlyphs[idx])
				whole++
			}
		}
	}
	if rest := width - whole; rest > 0 {
		b.WriteString(strings.Repeat("░", rest))
	}
	return b.String()
}

// indentBlock prefixes every line of a multi-line block, which plain string
// concatenation would only do for the first one.
func indentBlock(block string, spaces int) string {
	prefix := strings.Repeat(" ", spaces)
	lines := strings.Split(block, "\n")
	for i, line := range lines {
		if line == "" {
			continue
		}
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}

// cell pads plain text to exactly width cells, truncating with an ellipsis
// when it does not fit. Call it before applying style so padding counts
// visible cells rather than escape sequences.
func cell(s string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) > width {
		if width == 1 {
			return "…"
		}
		return string(runes[:width-1]) + "…"
	}
	return s + strings.Repeat(" ", width-len(runes))
}
