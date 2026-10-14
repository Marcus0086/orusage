package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/Marcus0086/orusage/internal/auth"
	"github.com/Marcus0086/orusage/internal/openrouter"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// labelWidth is the column every labeled row aligns its value to.
const labelWidth = 16

// joinWithin joins as many parts as fit in budget, always keeping the first
// and dropping the rest once one no longer fits, then truncates the result so
// a caller can render it without wrapping.
func joinWithin(budget int, parts ...string) string {
	if budget <= 0 {
		return ""
	}
	joined := ""
	for i, part := range parts {
		if part == "" {
			continue
		}
		if i == 0 {
			joined = part
			continue
		}
		if lipgloss.Width(joined)+3+lipgloss.Width(part) > budget {
			break
		}
		joined += " · " + part
	}
	return cell(joined, budget)
}

// View implements tea.Model.
func (m Model) View() string {
	if m.mode == ModeKeyPrompt {
		return m.keyPromptView()
	}
	return m.dashboardView()
}

func (m Model) viewWidth() int {
	w := m.width
	if w <= 0 {
		w = 100
	}
	if w > 118 {
		w = 118
	}
	return max(w, 40)
}

// dashboardView renders the live usage screen.
func (m Model) dashboardView() string {
	inner := m.viewWidth() - 2
	var b strings.Builder

	b.WriteString(m.headerView(inner))
	b.WriteString("\n")

	if line := m.ErrorLine(); line != "" {
		b.WriteString("\n  " + m.styles.ErrorText.Render(cell("⚠ "+line, inner)) + "\n")
	}

	if !m.hasInfo {
		msg := "  " + m.styles.Dim.Render(cell("reading key state…", inner))
		if m.inFlight {
			msg = "  " + m.spinner() + " " + m.styles.Dim.Render("contacting openrouter.ai…")
		}
		b.WriteString("\n" + msg + "\n")
		b.WriteString(m.footerView(inner))
		return b.String()
	}

	b.WriteString("\n" + m.bindingBudgetView(inner))
	b.WriteString("\n" + m.budgetsView(inner))
	b.WriteString("\n" + m.usageView(inner))
	b.WriteString("\n" + m.requestsView(inner))
	if section := m.historyView(inner); section != "" {
		b.WriteString("\n" + section)
	}
	if line := m.probeView(inner); line != "" {
		b.WriteString("\n" + line)
	}
	b.WriteString(m.footerView(inner))
	return b.String()
}

func (m Model) spinner() string {
	idx := int(m.now.UnixMilli()/100) % len(spinnerFrames)
	return m.styles.Accent.Render(spinnerFrames[idx])
}

// headerView renders the title, the live status, and the account line. The
// title and status shorten until they fit, so no terminal width is unusable.
func (m Model) headerView(inner int) string {
	fullTitle := m.styles.Title.Render("orusage") + m.styles.Faint.Render(" · OpenRouter usage")
	shortTitle := m.styles.Title.Render("orusage")
	status, shortStatus := m.statusView(), m.statusShortView()

	title := fullTitle
	if lipgloss.Width(title)+lipgloss.Width(status)+3 > inner {
		title = shortTitle
	}
	if lipgloss.Width(title)+lipgloss.Width(status)+3 > inner {
		status = shortStatus
	}
	if lipgloss.Width(title)+lipgloss.Width(status)+3 > inner {
		status = ""
	}

	first := "  " + title
	if pad := inner - lipgloss.Width(title) - lipgloss.Width(status) - 1; status != "" && pad > 0 {
		first += strings.Repeat(" ", pad) + status
	} else if status != "" {
		first += " " + status
	}
	return first + "\n" + m.accountView(inner)
}

func (m Model) statusView() string {
	label := m.statusLabel()
	switch {
	case m.paused:
		return m.styles.Warn.Render("⏸ " + label)
	case m.backoff.Attempt() > 0:
		return m.styles.Bad.Render("⏳ " + label)
	case m.inFlight:
		return m.styles.Accent.Render(m.spinner() + " " + label)
	default:
		return m.styles.Good.Render("● " + label)
	}
}

// statusShortView is the status reduced to its shortest useful form.
func (m Model) statusShortView() string {
	switch {
	case m.paused:
		return m.styles.Warn.Render("⏸")
	case m.backoff.Attempt() > 0:
		return m.styles.Bad.Render("⏳ " + HumanDuration(m.nextPoll.Sub(m.now)))
	case m.inFlight:
		return m.spinner()
	case !m.lastOK.IsZero():
		return m.styles.Good.Render("● " + Ago(m.now.Sub(m.lastOK)))
	default:
		return ""
	}
}

func (m Model) accountView(inner int) string {
	var parts []string
	switch {
	case m.key.Value != "":
		parts = append(parts, "key "+auth.Mask(m.key.Value))
	case m.info.Label != "":
		parts = append(parts, "key "+m.info.Label)
	}
	if m.info.WorkspaceID != "" {
		parts = append(parts, "workspace "+shortID(m.info.WorkspaceID))
	}
	if m.hasInfo {
		if m.info.IsFreeTier {
			parts = append(parts, "free tier")
		} else {
			parts = append(parts, "paid tier")
		}
	}
	if m.info.ExpiresAt != nil {
		parts = append(parts, "key expires "+Date(*m.info.ExpiresAt))
	}
	if m.key.Source != "" {
		parts = append(parts, "from "+m.key.Source)
	}
	if len(parts) == 0 {
		return ""
	}
	return m.styles.Subtitle.Render("  " + joinWithin(inner-2, parts...))
}

func shortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

// bindingBudgetView is the hero gauge: the budget that currently limits spend.
func (m Model) bindingBudgetView(inner int) string {
	budget := m.info.EffectiveBudget
	scope, interval := "key limit", m.info.LimitReset
	if budget != nil {
		scope, interval = budget.Scope, budget.ResetInterval
	}

	meta := []string{scope}
	if interval != "" {
		meta = append(meta, "resets "+interval)
	}
	if resets := m.info.ResetsAt(); resets != nil {
		meta = append(meta, DateClock(*resets))
	}
	heading := "  " + m.styles.Section.Render("Binding budget")
	if text := joinWithin(inner-18, meta...); text != "" {
		heading += "  " + m.styles.Faint.Render(text)
	}

	limit := m.info.LimitUSD()
	spend := m.info.Spend()
	if limit == nil {
		text := joinWithin(inner-2, "no spend limit on this key", Money(spend)+" spent all time")
		return heading + "\n  " + m.styles.Dim.Render(text) + "\n"
	}

	fraction := Share(spend, *limit)
	barWidth := clamp(inner-74, 10, 36)
	style := m.styles.forFraction(fraction)
	bar := style.Render(Bar(fraction, barWidth))

	parts := []string{fmt.Sprintf("%s of %s", Money(spend), MoneyFixed(*limit))}
	if remaining := *limit - spend; remaining < 0 {
		parts = append(parts, Money(-remaining)+" over")
	} else if inner >= 76 {
		parts = append(parts, Money(remaining)+" left")
	}
	if resets := m.info.ResetsAt(); resets != nil {
		parts = append(parts, "resets "+In(time.Until(*resets)))
	}
	detail := m.styles.Value.Render(joinWithin(inner-barWidth-14, parts...))

	line := "  " + bar + "  " + style.Render(cell(PercentText(spend, *limit), 8)) + "  " + detail
	return heading + "\n" + line + "\n"
}

// budgetsView lists every budget the key endpoint reports, shedding columns as
// the terminal narrows.
func (m Model) budgetsView(inner int) string {
	budgets := m.info.Budgets
	if len(budgets) == 0 && m.info.EffectiveBudget != nil {
		budgets = []openrouter.Budget{*m.info.EffectiveBudget}
	}

	var b strings.Builder
	b.WriteString("  " + m.styles.Section.Render("Budgets") + "\n")
	if len(budgets) == 0 {
		return b.String() + "    " + m.styles.Dim.Render("none reported") + "\n"
	}

	scopeWidth, intervalWidth, barWidth, percentWidth := 24, 8, 18, 8
	switch {
	case inner >= 108:
	case inner >= 88:
		scopeWidth, barWidth = 22, 12
	case inner >= 64:
		scopeWidth, intervalWidth, barWidth, percentWidth = 18, 0, 8, 7
	case inner >= 50:
		scopeWidth, intervalWidth, barWidth, percentWidth = 14, 0, 6, 7
	default:
		scopeWidth, intervalWidth, barWidth, percentWidth = 13, 0, 0, 7
	}
	prefix := 4 + scopeWidth + 2 + percentWidth
	if intervalWidth > 0 {
		prefix += 1 + intervalWidth
	}
	if barWidth > 0 {
		prefix += 2 + barWidth
	}

	for _, budget := range budgets {
		fraction := Share(budget.SpendUSD, budget.LimitUSD)
		style := m.styles.forFraction(fraction)

		row := "    " + cell(budget.Scope, scopeWidth)
		if intervalWidth > 0 {
			row += " " + cell(budget.ResetInterval, intervalWidth)
		}
		if barWidth > 0 {
			row += "  " + style.Render(Bar(fraction, barWidth))
		}
		row += "  " + style.Render(cell(PercentText(budget.SpendUSD, budget.LimitUSD), percentWidth))

		parts := []string{fmt.Sprintf("%s of %s", Money(budget.SpendUSD), MoneyFixed(budget.LimitUSD))}
		if over := budget.SpendUSD - budget.LimitUSD; over > 0 {
			parts = append(parts, Money(over)+" over")
		} else if inner >= 88 {
			parts = append(parts, Money(max(0, budget.RemainingUSD))+" left")
		}
		if budget.ResetsAt != nil {
			if inner >= 108 {
				parts = append(parts, "resets "+Date(*budget.ResetsAt))
			}
			parts = append(parts, In(time.Until(*budget.ResetsAt)))
		}
		if detail := joinWithin(inner-prefix-2, parts...); detail != "" {
			row += "  " + m.styles.Value.Render(detail)
		}
		b.WriteString(row + "\n")
	}
	return b.String()
}

// usageView is the credit table across time windows.
func (m Model) usageView(inner int) string {
	all := []struct {
		label string
		value float64
	}{
		{"today", m.info.UsageDaily},
		{"week", m.info.UsageWeekly},
		{"month", m.info.UsageMonthly},
		{"all time", m.info.Usage},
	}
	columns := 4
	switch {
	case inner < 58:
		columns = 2
	case inner < 76:
		columns = 3
	}
	shown := all[:columns]

	var b strings.Builder
	b.WriteString("  " + m.styles.Section.Render("Spend") + "\n")
	width := clamp((inner-18)/columns, 6, 18)

	header := "    " + cell("credits", 14)
	for _, column := range shown {
		header += m.styles.Faint.Render(cell(column.label, width))
	}
	b.WriteString(header + "\n")

	row := "    " + cell("", 14)
	for _, column := range shown {
		row += m.styles.Value.Render(cell(Money(column.value), width))
	}
	b.WriteString(row + "\n")

	if m.info.BYOKUsage > 0 || m.info.BYOKUsageMonthly > 0 {
		byok := "    " + cell("BYOK credits", 14)
		for _, column := range shown {
			value := column.value
			switch column.label {
			case "today":
				value = m.info.BYOKUsageDaily
			case "week":
				value = m.info.BYOKUsageWeekly
			case "month":
				value = m.info.BYOKUsageMonthly
			case "all time":
				value = m.info.BYOKUsage
			}
			byok += m.styles.Dim.Render(cell(Money(value), width))
		}
		b.WriteString(byok + "\n")
	}
	return b.String()
}

// requestsView is the rate-limit and health panel: what has been sent, how
// fast it may go, and what the API revealed about limits.
func (m Model) requestsView(inner int) string {
	var b strings.Builder
	b.WriteString("  " + m.styles.Section.Render("Requests & limits") + "\n")

	p50 := "n/a"
	if d, ok := m.telemetry.Percentile(0.5); ok {
		p50 = d.Round(time.Millisecond).String()
	}
	lastStatus := "—"
	if m.telemetry.LastStatus > 0 {
		lastStatus = fmt.Sprintf("HTTP %d", m.telemetry.LastStatus)
	}
	b.WriteString(m.row("Session", fmt.Sprintf("%d sent · %d failed · %d × 429 · last %s · p50 %s",
		m.telemetry.Requests, m.telemetry.Failures, m.telemetry.RateLimited, lastStatus, p50), inner))

	capText := "no cap"
	if perMinute := m.pacer.PerMinute(); perMinute > 0 {
		capText = fmt.Sprintf("%d/min", perMinute)
	}
	pace := []string{"cap " + capText, "poll every " + HumanDuration(m.effectiveInterval())}
	if m.paused {
		pace = append(pace, "paused · press p to resume")
	} else {
		pace = append(pace, "next "+In(m.nextPoll.Sub(m.now)))
	}
	if m.inBurst() {
		pace = append(pace, "burst")
	}
	if m.cappedByLimiter() {
		pace = append(pace, "held back by request cap")
	}
	b.WriteString(m.row("Pacing", strings.Join(pace, " · "), inner))

	b.WriteString(m.row("Rate limits", m.rateLimitText(), inner))

	if quota := m.info.FreeModelDailyRequests; quota != nil && quota.Limit > 0 {
		available := inner - labelWidth - 4
		suffix := fmt.Sprintf("  %s / %s", Count(quota.Used), Count(quota.Limit))
		if len(suffix)+9 <= available {
			suffix += " today"
		}
		barWidth := clamp(available-len(suffix), 4, 28)
		fraction := Share(float64(quota.Used), float64(quota.Limit))
		bar := m.styles.forFraction(fraction).Render(Bar(fraction, barWidth))
		b.WriteString(m.styledRow("Free models", bar+m.styles.Value.Render(suffix)))
	}
	return b.String()
}

// row renders a labeled line, truncating the value to the available width.
func (m Model) row(label, value string, inner int) string {
	return m.styledRow(label, m.styles.Value.Render(cell(value, max(8, inner-labelWidth-4))))
}

// styledRow renders a labeled line whose value is already styled and sized by
// the caller.
func (m Model) styledRow(label, value string) string {
	return "    " + m.styles.Label.Render(cell(label, labelWidth)) + value + "\n"
}

func (m Model) rateLimitText() string {
	limits := m.obs.Limits
	if !limits.Absent {
		var parts []string
		if limits.Limit != nil {
			parts = append(parts, fmt.Sprintf("limit %d", *limits.Limit))
		}
		if limits.Remaining != nil {
			parts = append(parts, fmt.Sprintf("remaining %d", *limits.Remaining))
		}
		if limits.Reset != nil {
			parts = append(parts, "resets "+In(time.Until(*limits.Reset)))
		}
		if limits.HasRetryAfter {
			parts = append(parts, "retry-after "+limits.RetryAfter.String())
		}
		if len(parts) > 0 {
			return strings.Join(parts, " · ")
		}
	}
	if rl := m.info.RateLimit; rl != nil && rl.Requests > 0 {
		return fmt.Sprintf("API reports %d requests per %s (deprecated field)", rl.Requests, rl.Interval)
	}
	return "none published by the API (no x-ratelimit headers)"
}

// historyView shows movement over time: a sparkline plus a burn-rate estimate.
// It renders nothing until there is enough history to be worth a row, because
// a one-point sparkline and a placeholder burn rate are noise on a screen that
// someone just opened.
func (m Model) historyView(inner int) string {
	series := m.historySeries()
	window := m.trendWindow()
	hasTrend := len(series) >= minTrendSamples && window >= minTrendWindow
	rate, hasRate := m.burnRate()
	if !hasTrend && !hasRate {
		return ""
	}

	var b strings.Builder
	b.WriteString("  " + m.styles.Section.Render("History") + "\n")

	if hasTrend {
		label := "remaining"
		if m.info.LimitUSD() == nil {
			label = "usage"
		}
		window := HumanDuration(m.trendWindow())

		// The sparkline absorbs whatever the trailing caption does not use, so
		// the caption shortens before the row is allowed to overflow.
		const minSpark = 6
		available := inner - labelWidth - 4
		trailer := ""
		for _, candidate := range []string{
			fmt.Sprintf("%s over %s · %s", label, window, plural(len(m.samples), "sample")),
			fmt.Sprintf("%s over %s", label, window),
			window,
		} {
			if len(candidate)+minSpark+2 <= available {
				trailer = candidate
				break
			}
		}
		sparkWidth := clamp(available-len(trailer)-2, minSpark, 52)
		if trailer == "" {
			sparkWidth = clamp(available, minSpark, 52)
		}
		spark := m.styles.Accent.Render(Sparkline(series, sparkWidth))
		b.WriteString(m.styledRow("Trend", spark+m.styles.Faint.Render("  "+trailer)))
	}

	if hasRate {
		parts := []string{fmt.Sprintf("%s/hr ≈ %s/day", Money(rate), Money(rate*24))}
		if session, ok := m.sessionSpend(); ok {
			parts = append(parts, Money(session)+" this session")
		}
		if remaining, ok := m.remaining(); ok && rate > 0 {
			hours := remaining / rate
			parts = append(parts, "limit reached "+In(time.Duration(hours*float64(time.Hour))))
		}
		b.WriteString(m.row("Burn rate", strings.Join(parts, " · "), inner))
	}
	return b.String()
}

func (m Model) probeView(inner int) string {
	available := max(8, inner-labelWidth-4)
	switch {
	case m.probing:
		return m.styledRow("Test request", m.spinner()+" "+m.styles.Dim.Render(cell("sending one-token "+m.cfg.ProbeModel+" request…", available-2)))
	case m.probeErr != nil:
		return m.styledRow("Test request", m.styles.ErrorText.Render(cell(m.probeErr.Error(), available)))
	case m.probe != nil:
		text := fmt.Sprintf("%s via %s · %s tokens · %s · %s",
			m.probe.Model, m.probe.Provider, Count(m.probe.TotalTokens), Money(m.probe.CostUSD),
			m.probeObs.Latency.Round(time.Millisecond))
		if m.probe.Text != "" {
			text += fmt.Sprintf(" · %q", truncateRunes(m.probe.Text, 24))
		}
		return m.styledRow("Test request", m.styles.Value.Render(cell(text, available)))
	}
	return ""
}

// binding is one key hint; essential hints survive a narrow terminal.
type binding struct {
	key, desc string
	essential bool
}

// renderBindings joins key hints within budget: essential hints are always
// shown, optional ones only while they fit, so the hint line never wraps.
func (m Model) renderBindings(bindings []binding, budget int) string {
	separator := m.styles.Faint.Render(" · ")
	separatorWidth := lipgloss.Width(separator)

	essentialWidth, essentialCount := 0, 0
	for _, bind := range bindings {
		if bind.essential {
			essentialWidth += m.bindingWidth(bind)
			essentialCount++
		}
	}
	available := budget - essentialWidth
	if essentialCount > 1 {
		available -= separatorWidth * (essentialCount - 1)
	}

	included := make([]binding, 0, len(bindings))
	for _, bind := range bindings {
		if bind.essential {
			included = append(included, bind)
			continue
		}
		if width := m.bindingWidth(bind) + separatorWidth; width <= available {
			available -= width
			included = append(included, bind)
		}
	}

	for {
		parts := make([]string, 0, len(included))
		for _, bind := range included {
			parts = append(parts, m.styles.Key.Render(bind.key)+" "+m.styles.KeyDesc.Render(bind.desc))
		}
		line := strings.Join(parts, separator)
		if lipgloss.Width(line) <= budget {
			return line
		}
		// Drop the last optional hint and re-check; with only essentials left
		// there is nothing more to give up.
		drop := -1
		for i := len(included) - 1; i >= 0; i-- {
			if !included[i].essential {
				drop = i
				break
			}
		}
		if drop < 0 {
			return line
		}
		included = append(included[:drop], included[drop+1:]...)
	}
}

func (m Model) bindingWidth(bind binding) int {
	return lipgloss.Width(bind.key) + 1 + lipgloss.Width(bind.desc)
}

func (m Model) footerView(inner int) string {
	var b strings.Builder
	if m.flash != "" {
		b.WriteString("\n  " + m.styles.Flash.Render(cell(m.flash, inner)) + "\n")
	}
	b.WriteString("\n  " + m.styles.Rule.Render(strings.Repeat("─", max(8, inner-2))) + "\n")
	if m.showHelp {
		return b.String() + m.helpView(inner)
	}

	bindings := []binding{
		{"r", "refresh", true},
		{"p", "pause", true},
		{"?", "help", true},
		{"q", "quit", true},
		{"k", "API key", false},
		{"+/-", "poll rate", false},
	}
	if m.cfg.ProbeEnabled {
		bindings = append(bindings, binding{"t", "test request", false})
	}
	b.WriteString("  " + m.renderBindings(bindings, inner-2) + "\n")
	return b.String()
}

func (m Model) helpView(inner int) string {
	lines := [][2]string{
		{"r", "poll openrouter.ai immediately"},
		{"p / space", "pause or resume polling"},
		{"+ / -", "halve or double the poll interval"},
		{"k", "enter a different API key"},
		{"t", "send one minimal completion to test the key"},
		{"esc / ?", "close this help"},
		{"q / ctrl+c", "quit"},
	}
	var b strings.Builder
	for _, line := range lines {
		b.WriteString("    " + m.styles.Key.Render(cell(line[0], 12)) + m.styles.KeyDesc.Render(cell(line[1], max(8, inner-16))) + "\n")
	}
	b.WriteString("    " + m.styles.Faint.Render(cell("spend and quota come from GET /api/v1/key; the screen refreshes on its own", inner-4)) + "\n")
	return b.String()
}

// keyPromptView asks for a key when none could be resolved.
func (m Model) keyPromptView() string {
	inner := max(36, m.viewWidth()-2)
	var b strings.Builder
	b.WriteString("  " + m.styles.Title.Render("orusage") + m.styles.Faint.Render(" · OpenRouter usage") + "\n\n")

	if m.key.Value != "" {
		b.WriteString("  " + m.styles.Section.Render("Enter a different API key") + " " +
			m.styles.Faint.Render("current: "+auth.Mask(m.key.Value)) + "\n\n")
	} else {
		b.WriteString("  " + m.styles.Section.Render("No API key found") + "\n\n")
	}

	b.WriteString("  " + m.styles.Dim.Render(cell("Paste an OpenRouter key (sk-or-…). It is held in memory unless you save it.", inner)) + "\n\n")
	box := m.styles.PromptBox.Width(max(16, min(60, inner-6))).Render(m.input.View())
	b.WriteString(indentBlock(box, 2) + "\n\n")

	if !m.hasInfo {
		b.WriteString("  " + m.styles.Faint.Render("searched:") + "\n")
		for i, path := range auth.SearchPath() {
			if i >= 5 {
				b.WriteString("    " + m.styles.Faint.Render("…") + "\n")
				break
			}
			b.WriteString("    " + m.styles.Faint.Render(cell(path, inner-4)) + "\n")
		}
		b.WriteString("\n")
	}

	if m.flash != "" {
		b.WriteString("  " + m.styles.Flash.Render(cell(m.flash, inner)) + "\n\n")
	}
	b.WriteString("  " + m.renderBindings([]binding{
		{"enter", "connect", true},
		{"ctrl+s", "save this key", false},
		{"esc", "quit", true},
	}, inner-2) + "\n")
	return b.String()
}

func truncateRunes(s string, width int) string {
	runes := []rune(s)
	if len(runes) <= width {
		return s
	}
	return string(runes[:width]) + "…"
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
