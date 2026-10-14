package ui

import (
	"math"
	"strings"
	"testing"
	"time"
)

func TestMoneyPrecision(t *testing.T) {
	cases := map[float64]string{
		0:            "$0.00",
		0.0000087:    "$0.000009", // one cheap request must not read as free
		0.00018:      "$0.0002",
		0.097030092:  "$0.097",
		41.361356898: "$41.36",
		709.92:       "$709.92",
	}
	for input, want := range cases {
		if got := Money(input); got != want {
			t.Errorf("Money(%v) = %q, want %q", input, got, want)
		}
	}
	if got := MoneyFixed(41.361356898); got != "$41.36" {
		t.Errorf("MoneyFixed = %q", got)
	}
}

func TestCount(t *testing.T) {
	cases := map[int]string{0: "0", 12: "12", 999: "999", 1000: "1,000", 1234567: "1,234,567", -1500: "-1,500"}
	for input, want := range cases {
		if got := Count(input); got != want {
			t.Errorf("Count(%d) = %q, want %q", input, got, want)
		}
	}
}

func TestPercentTextKeepsSmallFractions(t *testing.T) {
	cases := []struct {
		spend, limit float64
		want         string
	}{
		{0, 250, "0.00%"},
		{0.02, 250, "0.0080%"},
		{1, 250, "0.40%"},
		{41.36, 250, "16.5%"},
		{250, 250, "100.0%"},
		{5, 0, "  n/a"}, // an unlimited key has no percentage
	}
	for _, c := range cases {
		if got := PercentText(c.spend, c.limit); got != c.want {
			t.Errorf("PercentText(%v, %v) = %q, want %q", c.spend, c.limit, got, c.want)
		}
	}
}

func TestShareClamps(t *testing.T) {
	cases := map[[2]float64]float64{
		{0, 100}:   0,
		{50, 100}:  0.5,
		{100, 100}: 1,
		{150, 100}: 1, // past the limit the bar is full, never wider
		{50, 0}:    0, // unlimited
		{-5, 100}:  0,
	}
	for input, want := range cases {
		if got := Share(input[0], input[1]); got != want {
			t.Errorf("Share(%v, %v) = %v, want %v", input[0], input[1], got, want)
		}
	}
}

func TestHumanDuration(t *testing.T) {
	cases := map[time.Duration]string{
		0:                   "now",
		-5 * time.Second:    "now",
		45 * time.Second:    "45s",
		90 * time.Second:    "1m 30s",
		2 * time.Hour:       "2h 00m",
		26 * time.Hour:      "1d 2h",
		18 * 24 * time.Hour: "18d 0h",
	}
	for input, want := range cases {
		if got := HumanDuration(input); got != want {
			t.Errorf("HumanDuration(%v) = %q, want %q", input, got, want)
		}
	}
	if got := In(-time.Second); got != "now" {
		t.Errorf("In(past) = %q, want now", got)
	}
	if got := In(90 * time.Second); got != "in 1m 30s" {
		t.Errorf("In(90s) = %q", got)
	}
}

func TestAgo(t *testing.T) {
	cases := map[time.Duration]string{
		-1 * time.Second:        "0ms ago",
		250 * time.Millisecond:  "250ms ago",
		1500 * time.Millisecond: "1.5s ago",
		90 * time.Second:        "1m ago",
		2 * time.Hour:           "2h ago",
	}
	for input, want := range cases {
		if got := Ago(input); got != want {
			t.Errorf("Ago(%v) = %q, want %q", input, got, want)
		}
	}
}

func TestBarKeepsExactWidth(t *testing.T) {
	for _, width := range []int{1, 4, 10, 25, 40} {
		for _, fraction := range []float64{-1, 0, 0.003, 0.42, 0.999, 1, 1.7} {
			bar := Bar(fraction, width)
			if got := len([]rune(bar)); got != width {
				t.Errorf("Bar(%v, %d) rendered %d cells: %q", fraction, width, got, bar)
			}
		}
	}
	if got := Bar(0.5, 0); got != "" {
		t.Errorf("Bar with zero width = %q", got)
	}
	// A non-zero fraction must show something even in a single cell.
	if got := Bar(0.01, 1); got == "░" {
		t.Error("a small non-zero fraction rendered as empty")
	}
}

func TestSparklineShape(t *testing.T) {
	if got := Sparkline(nil, 10); got != "" {
		t.Errorf("Sparkline(nil) = %q", got)
	}
	values := []float64{1, 2, 3, 4, 5}
	if got := Sparkline(values, 5); got != "▁▂▄▆█" {
		t.Errorf("Sparkline = %q", got)
	}
	if got := len([]rune(Sparkline(values, 3))); got != 3 {
		t.Errorf("Sparkline ignored the width: %d cells", got)
	}
	flat := []float64{2, 2, 2}
	if got := Sparkline(flat, 3); got != "▁▁▁" {
		t.Errorf("flat series = %q, want the lowest glyph rather than a divide by zero", got)
	}
}

func TestCellPadsAndTruncates(t *testing.T) {
	if got := cell("ab", 4); got != "ab  " {
		t.Errorf("cell padded = %q", got)
	}
	if got := cell("abcdef", 4); got != "abc…" {
		t.Errorf("cell truncated = %q", got)
	}
	if got := cell("x", 0); got != "" {
		t.Errorf("cell with zero width = %q", got)
	}
	if got := cell("abc", 1); got != "…" {
		t.Errorf("cell at one cell = %q", got)
	}
}

// TestIndicatorGlyphsAreSingleWidth guards the layout invariant: every glyph
// the dashboard draws must occupy exactly one cell, or the columns drift.
func TestIndicatorGlyphsAreSingleWidth(t *testing.T) {
	for _, glyph := range []string{"●", "⏸", "⏳", "⚠", "…", "█", "░", "▏", "▉"} {
		if got := len([]rune(glyph)); got != 1 {
			t.Errorf("%q spans %d runes", glyph, got)
		}
	}
	for _, frame := range spinnerFrames {
		if got := len([]rune(frame)); got != 1 {
			t.Errorf("spinner frame %q spans %d runes", frame, got)
		}
	}
	if math.IsNaN(Share(1, 1)) {
		t.Error("Share produced NaN")
	}
	if !strings.Contains(Bar(1, 4), "█") {
		t.Error("a full bar rendered no filled cells")
	}
}
