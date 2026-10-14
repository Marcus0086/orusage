package ui

import "github.com/charmbracelet/lipgloss"

// Palette holds the colours used across the dashboard, with light-terminal
// variants so the screen stays readable on either background.
type Palette struct {
	Accent lipgloss.AdaptiveColor
	Text   lipgloss.AdaptiveColor
	Dim    lipgloss.AdaptiveColor
	Faint  lipgloss.AdaptiveColor
	Good   lipgloss.AdaptiveColor
	Warn   lipgloss.AdaptiveColor
	Bad    lipgloss.AdaptiveColor
	Crit   lipgloss.AdaptiveColor
}

// DefaultPalette is a warm, terminal-friendly scheme with explicit light and
// dark values.
func DefaultPalette() Palette {
	return Palette{
		Accent: lipgloss.AdaptiveColor{Light: "#B45309", Dark: "#F0A868"},
		Text:   lipgloss.AdaptiveColor{Light: "#1F2937", Dark: "#E5E7EB"},
		Dim:    lipgloss.AdaptiveColor{Light: "#6B7280", Dark: "#9CA3AF"},
		Faint:  lipgloss.AdaptiveColor{Light: "#9CA3AF", Dark: "#6B7280"},
		Good:   lipgloss.AdaptiveColor{Light: "#15803D", Dark: "#6EE7A8"},
		Warn:   lipgloss.AdaptiveColor{Light: "#A16207", Dark: "#FBBF24"},
		Bad:    lipgloss.AdaptiveColor{Light: "#C2410C", Dark: "#FB923C"},
		Crit:   lipgloss.AdaptiveColor{Light: "#B91C1C", Dark: "#F87171"},
	}
}

// Styles bundles the rendered text styles for a screen.
type Styles struct {
	Title     lipgloss.Style
	Subtitle  lipgloss.Style
	Section   lipgloss.Style
	Label     lipgloss.Style
	Value     lipgloss.Style
	Strong    lipgloss.Style
	Dim       lipgloss.Style
	Faint     lipgloss.Style
	Good      lipgloss.Style
	Warn      lipgloss.Style
	Bad       lipgloss.Style
	Crit      lipgloss.Style
	Accent    lipgloss.Style
	Rule      lipgloss.Style
	Flash     lipgloss.Style
	Key       lipgloss.Style
	KeyDesc   lipgloss.Style
	ErrorText lipgloss.Style
	PromptBox lipgloss.Style
}

// NewStyles derives the screen styles from a palette.
func NewStyles(p Palette) Styles {
	return Styles{
		Title:     lipgloss.NewStyle().Foreground(p.Accent).Bold(true),
		Subtitle:  lipgloss.NewStyle().Foreground(p.Dim),
		Section:   lipgloss.NewStyle().Foreground(p.Text).Bold(true),
		Label:     lipgloss.NewStyle().Foreground(p.Dim),
		Value:     lipgloss.NewStyle().Foreground(p.Text),
		Strong:    lipgloss.NewStyle().Foreground(p.Text).Bold(true),
		Dim:       lipgloss.NewStyle().Foreground(p.Dim),
		Faint:     lipgloss.NewStyle().Foreground(p.Faint),
		Good:      lipgloss.NewStyle().Foreground(p.Good),
		Warn:      lipgloss.NewStyle().Foreground(p.Warn),
		Bad:       lipgloss.NewStyle().Foreground(p.Bad),
		Crit:      lipgloss.NewStyle().Foreground(p.Crit),
		Accent:    lipgloss.NewStyle().Foreground(p.Accent),
		Rule:      lipgloss.NewStyle().Foreground(p.Faint),
		Flash:     lipgloss.NewStyle().Foreground(p.Accent),
		Key:       lipgloss.NewStyle().Foreground(p.Accent).Bold(true),
		KeyDesc:   lipgloss.NewStyle().Foreground(p.Dim),
		ErrorText: lipgloss.NewStyle().Foreground(p.Crit),
		PromptBox: lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(p.Faint).Padding(0, 1),
	}
}

// forFraction picks the severity style for a used fraction: quiet while there
// is room, increasingly loud as the budget fills.
func (s Styles) forFraction(fraction float64) lipgloss.Style {
	switch {
	case fraction >= 0.95:
		return s.Crit
	case fraction >= 0.8:
		return s.Bad
	case fraction >= 0.5:
		return s.Warn
	default:
		return s.Good
	}
}
