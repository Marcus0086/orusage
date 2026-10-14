package ui

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// TestGaugeColorBands pins the thresholds the bar switches at: green while
// there is room, yellow from half, orange from 80%, red from 95% and past the
// limit.
func TestGaugeColorBands(t *testing.T) {
	s := NewStyles(DefaultPalette())
	cases := []struct {
		name     string
		fraction float64
		want     lipgloss.Style
	}{
		{"untouched", 0, s.Good},
		{"just under half", 0.499, s.Good},
		{"at half", 0.5, s.Warn},
		{"just under 80%", 0.799, s.Warn},
		{"at 80%", 0.8, s.Bad},
		{"just under 95%", 0.949, s.Bad},
		{"at 95%", 0.95, s.Crit},
		{"at the limit", 1, s.Crit},
		{"past the limit", 1.4, s.Crit},
	}
	for _, c := range cases {
		got := s.forFraction(c.fraction)
		if got.Render("x") != c.want.Render("x") {
			t.Errorf("%s (%.3f) selected the wrong band", c.name, c.fraction)
		}
	}
}

// TestPaletteProgressesGreenToRed checks the four gauge colours really are
// green, yellow, orange, and red, and that they step down in hue in that
// order, so the bar reads as a single progression rather than four arbitrary
// colours.
func TestPaletteProgressesGreenToRed(t *testing.T) {
	p := DefaultPalette()
	bounds := []struct {
		name      string
		color     lipgloss.AdaptiveColor
		low, high float64
	}{
		{"green", p.Good, 90, 170},
		{"yellow", p.Warn, 35, 60},
		{"orange", p.Bad, 15, 35},
		{"red", p.Crit, 0, 15},
	}

	// Each terminal variant is checked on its own: the light and dark colours
	// are allowed to differ, but both must run green → yellow → orange → red.
	variants := []struct {
		name string
		pick func(lipgloss.AdaptiveColor) string
	}{
		{"light", func(c lipgloss.AdaptiveColor) string { return c.Light }},
		{"dark", func(c lipgloss.AdaptiveColor) string { return c.Dark }},
	}
	for _, variant := range variants {
		previous := math.Inf(1)
		for _, bound := range bounds {
			hex := variant.pick(bound.color)
			hue, err := hueOf(hex)
			if err != nil {
				t.Fatalf("%s: %v", bound.name, err)
			}
			if hue < bound.low || hue > bound.high {
				t.Errorf("%s %s (%s) has hue %.0f°, want %.0f–%.0f°", bound.name, hex, variant.name, hue, bound.low, bound.high)
			}
			if hue >= previous {
				t.Errorf("%s %s (%s) has hue %.0f°, which does not continue the progression from %.0f°", bound.name, hex, variant.name, hue, previous)
			}
			previous = hue
		}
	}
}

// hueOf returns the hue angle in degrees for a #RRGGBB colour.
func hueOf(hex string) (float64, error) {
	trimmed := strings.TrimPrefix(hex, "#")
	if len(trimmed) != 6 {
		return 0, fmt.Errorf("colour %q is not #RRGGBB", hex)
	}
	channel := func(i int) (float64, error) {
		value, err := strconv.ParseUint(trimmed[i:i+2], 16, 8)
		if err != nil {
			return 0, err
		}
		return float64(value) / 255, nil
	}
	r, err := channel(0)
	if err != nil {
		return 0, err
	}
	g, err := channel(2)
	if err != nil {
		return 0, err
	}
	b, err := channel(4)
	if err != nil {
		return 0, err
	}

	maxc := math.Max(r, math.Max(g, b))
	minc := math.Min(r, math.Min(g, b))
	if maxc == minc {
		return 0, fmt.Errorf("colour %q is greyscale", hex)
	}
	delta := maxc - minc
	var hue float64
	switch maxc {
	case r:
		hue = math.Mod((g-b)/delta, 6)
	case g:
		hue = (b-r)/delta + 2
	default:
		hue = (r-g)/delta + 4
	}
	hue *= 60
	if hue < 0 {
		hue += 360
	}
	return hue, nil
}
