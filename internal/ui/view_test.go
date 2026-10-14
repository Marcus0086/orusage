package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/raghavgupta/orusage/internal/auth"
	"github.com/raghavgupta/orusage/internal/openrouter"
)

const fixtureKey = "sk-or-v1-8c1d5c0f4e2b7a9d3c6f8b1e4a7d0c3f8d8"

func at(offset time.Duration) *time.Time {
	t := fixtureNow().Add(offset)
	return &t
}

func fixtureNow() time.Time {
	return time.Date(2026, 10, 14, 13, 30, 0, 0, time.UTC)
}

// fixture builds a dashboard holding a realistic payload so the rendered
// screen can be checked without network access.
func fixture() Model {
	m := New(Config{
		Client:       openrouter.New("http://127.0.0.1:9", fixtureKey),
		Key:          auth.Key{Value: fixtureKey, Source: "~/.env"},
		Interval:     5 * time.Second,
		MaxPerMinute: 30,
		ProbeModel:   "openai/gpt-4o-mini",
		ProbeEnabled: true,
	})
	m.now = fixtureNow()
	m.width, m.height = 110, 44
	m.hasInfo = true
	m.lastOK = m.now.Add(-1200 * time.Millisecond)
	m.info = openrouter.KeyInfo{
		Label:          "sk-or-v1-8c1...8d8",
		Limit:          ptr(250.0),
		LimitReset:     "monthly",
		LimitRemaining: ptr(208.638643102),
		WorkspaceID:    "69a4d90a-5539-59c0-abd8-f8941c021e60",
		ExpiresAt:      at(360 * 24 * time.Hour),
		Usage:          41.361356898,
		UsageDaily:     0.42,
		UsageWeekly:    33.966906289,
		UsageMonthly:   41.361356898,
		FreeModelDailyRequests: &openrouter.FreeModelDailyRequests{
			Used: 12, Limit: 1000, Remaining: 988,
		},
		RateLimit: &openrouter.DeprecatedRateLimit{
			Requests: -1, Interval: "10s", Note: "deprecated",
		},
		EffectiveBudget: &openrouter.Budget{
			Scope: "key_limit", LimitUSD: 250, SpendUSD: 41.361356898,
			RemainingUSD: 208.638643102, ResetInterval: "monthly", ResetsAt: at(18 * 24 * time.Hour),
		},
		Budgets: []openrouter.Budget{
			{
				Scope: "key_limit", LimitUSD: 250, SpendUSD: 41.361356898,
				RemainingUSD: 208.638643102, ResetInterval: "monthly", ResetsAt: at(18 * 24 * time.Hour),
			},
			{
				Scope: "workspace", LimitUSD: 200, SpendUSD: 0.097030092,
				RemainingUSD: 199.902969908, ResetInterval: "daily", ResetsAt: at(10 * time.Hour),
			},
			{
				Scope: "workspace_default_key", LimitUSD: 500, SpendUSD: 1.164521146,
				RemainingUSD: 498.835478854, ResetInterval: "weekly", ResetsAt: at(5 * 24 * time.Hour),
			},
		},
	}
	m.obs = openrouter.Observation{
		FetchedAt: m.now.Add(-1200 * time.Millisecond), Latency: 168 * time.Millisecond,
		StatusCode: 200, Path: "/key", Limits: openrouter.RateLimits{Absent: true},
	}
	m.telemetry = Telemetry{
		Requests: 42, LastStatus: 200,
		Latencies: []time.Duration{120 * time.Millisecond, 168 * time.Millisecond, 240 * time.Millisecond},
	}
	for i := 0; i < 24; i++ {
		m.samples = append(m.samples, sample{
			At:    m.now.Add(time.Duration(i-23) * 6 * time.Second),
			Spend: 41.30 + float64(i)*0.0026,
			Usage: 41.30 + float64(i)*0.0026,
			Daily: 0.42,
		})
	}
	m.nextPoll = m.now.Add(3 * time.Second)
	return m
}

func ptr[T any](v T) *T { return &v }

// TestDashboardFitsWidth checks the layout invariant that matters most: no
// rendered line is wider than the terminal.
func TestDashboardFitsWidth(t *testing.T) {
	for _, width := range []int{44, 52, 60, 72, 84, 92, 100, 110, 118} {
		m := fixture()
		m.width = width
		view := m.View()
		for i, line := range strings.Split(view, "\n") {
			if got := lipgloss.Width(line); got > width {
				t.Errorf("width %d: line %d is %d cells wide: %q", width, i, got, line)
			}
		}
	}
}

func TestDashboardShowsUsageFacts(t *testing.T) {
	m := fixture()
	m.width = 118
	view := m.View()
	for _, want := range []string{
		"orusage",
		"Binding budget",
		"key_limit",
		"$41.36 of $250.00",
		"$208.64 left",
		"$33.97",    // weekly spend
		"Budgets",   // budget table
		"workspace", // budget scope
		"Requests & limits",
		"30/min", // request cap
		"none published by the API",
		"12 / 1,000", // free model quota
		"History",
		"Burn rate",
		"sk-or-v1-8c1",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("dashboard is missing %q\n%s", want, view)
		}
	}
}

func TestOverLimitBudgetIsFlagged(t *testing.T) {
	m := fixture()
	m.width = 118
	m.info.EffectiveBudget.SpendUSD = 260
	m.info.EffectiveBudget.LimitUSD = 250
	m.info.EffectiveBudget.RemainingUSD = -10

	view := m.View()
	for _, want := range []string{"104.0%", "$10.00 over"} {
		if !strings.Contains(view, want) {
			t.Errorf("over-limit dashboard is missing %q\n%s", want, view)
		}
	}
	if strings.Contains(view, "$0.00 left") {
		t.Errorf("over-limit dashboard still reports a remaining balance\n%s", view)
	}
	// The bar itself is full rather than overshooting its column.
	if got := Share(260, 250); got != 1 {
		t.Errorf("Share(260, 250) = %v, want a full bar", got)
	}
}

func TestDashboardStatesRender(t *testing.T) {
	t.Run("loading", func(t *testing.T) {
		m := fixture()
		m.hasInfo = false
		if view := m.View(); !strings.Contains(view, "reading key state") {
			t.Errorf("loading view missing: %s", view)
		}
	})

	t.Run("paused", func(t *testing.T) {
		m := fixture()
		m.paused = true
		if view := m.View(); !strings.Contains(view, "paused") {
			t.Errorf("paused state not shown: %s", view)
		}
	})

	t.Run("unauthorized hint", func(t *testing.T) {
		m := fixture()
		m.lastErr = &openrouter.APIError{StatusCode: 401, Message: "No auth credentials found"}
		if view := m.View(); !strings.Contains(view, "press k") {
			t.Errorf("401 hint missing: %s", view)
		}
	})

	t.Run("rate limited", func(t *testing.T) {
		m := fixture()
		m.applyFetch(fetchMsg{err: &openrouter.APIError{StatusCode: 429, Message: "slow down", RetryAfter: 7 * time.Second, HasRetryAfter: true}})
		view := m.View()
		if !strings.Contains(view, "retry #1") {
			t.Errorf("backoff state not shown: %s", view)
		}
	})

	t.Run("probe result", func(t *testing.T) {
		m := fixture()
		m.probe = &openrouter.Probe{Model: "openai/gpt-4o-mini", Provider: "Azure", TotalTokens: 9, CostUSD: 0.0000018, Text: "Hello"}
		m.probeObs = openrouter.Observation{Latency: 1648 * time.Millisecond}
		if view := m.View(); !strings.Contains(view, "Azure") {
			t.Errorf("probe result not shown: %s", view)
		}
	})

	t.Run("help", func(t *testing.T) {
		m := fixture()
		m.showHelp = true
		if view := m.View(); !strings.Contains(view, "halve or double the poll interval") {
			t.Errorf("help overlay missing: %s", view)
		}
	})
}

func TestKeyPromptAcceptsEnteredKey(t *testing.T) {
	m := New(Config{Client: openrouter.New("http://127.0.0.1:9", ""), Interval: time.Second, MaxPerMinute: 30})
	m.width = 100
	if m.mode != ModeKeyPrompt {
		t.Fatal("an empty key should start the prompt")
	}
	if view := m.View(); !strings.Contains(view, "No API key found") {
		t.Errorf("prompt view missing: %s", view)
	}

	m.input.SetValue("  \"sk-or-v1-entered-key\"  ")
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := updated.(Model)
	if got.mode != ModeDashboard {
		t.Fatal("enter did not switch to the dashboard")
	}
	if got.cfg.Client.APIKey != "sk-or-v1-entered-key" {
		t.Errorf("client key = %q, want the trimmed input", got.cfg.Client.APIKey)
	}
	if got.key.Source != "entered interactively" {
		t.Errorf("source = %q", got.key.Source)
	}
}

func TestApplyingFetchRecordsMovement(t *testing.T) {
	m := fixture()
	m.samples = m.samples[:5]
	before := len(m.samples)
	next := m.info
	budgetCopy := *m.info.EffectiveBudget
	budgetCopy.SpendUSD += 0.5
	next.EffectiveBudget = &budgetCopy
	next.Usage += 0.5
	next.UsageDaily += 0.5

	updated, _ := m.applyFetch(fetchMsg{info: next, obs: openrouter.Observation{FetchedAt: m.now, StatusCode: 200, Latency: 150 * time.Millisecond}})
	got := updated.(Model)
	if len(got.samples) != before+1 {
		t.Errorf("samples = %d, want %d after spend moved", len(got.samples), before+1)
	}
	if !got.inBurst() {
		t.Error("spend movement should start a burst")
	}
	if got.currentInterval() >= got.interval {
		t.Errorf("burst interval %v is not faster than %v", got.currentInterval(), got.interval)
	}
	if !got.nextPoll.After(m.now) {
		t.Error("next poll was not scheduled")
	}

	// An unchanged payload must not add history.
	updated, _ = got.applyFetch(fetchMsg{info: next, obs: openrouter.Observation{FetchedAt: m.now, StatusCode: 200}})
	still := updated.(Model)
	if len(still.samples) != before+1 {
		t.Errorf("samples = %d, want no new sample for unchanged numbers", len(still.samples))
	}
}

func TestRateLimitBackoffSchedulesRetry(t *testing.T) {
	m := fixture()
	updated, _ := m.applyFetch(fetchMsg{err: &openrouter.APIError{StatusCode: 429, RetryAfter: 7 * time.Second, HasRetryAfter: true}})
	got := updated.(Model)
	if got.telemetry.RateLimited != 1 {
		t.Errorf("rate-limited count = %d, want 1", got.telemetry.RateLimited)
	}
	if delta := got.nextPoll.Sub(m.now); delta != 7*time.Second {
		t.Errorf("retry scheduled in %v, want the 7s Retry-After", delta)
	}
	if got.backoff.Attempt() != 1 {
		t.Errorf("backoff attempt = %d, want 1", got.backoff.Attempt())
	}

	// A success clears the streak.
	updated, _ = got.applyFetch(fetchMsg{info: got.info, obs: openrouter.Observation{FetchedAt: m.now, StatusCode: 200}})
	recovered := updated.(Model)
	if recovered.backoff.Attempt() != 0 || recovered.lastErr != nil {
		t.Errorf("recovery did not reset the backoff: attempt=%d err=%v", recovered.backoff.Attempt(), recovered.lastErr)
	}
}

func TestIntervalBoundsAndPacerSync(t *testing.T) {
	m := fixture()
	m.setInterval(10 * time.Millisecond)
	if m.interval != MinPollInterval {
		t.Errorf("interval = %v, want the %v floor", m.interval, MinPollInterval)
	}
	m.setInterval(2 * time.Hour)
	if m.interval != MaxPollInterval {
		t.Errorf("interval = %v, want the %v ceiling", m.interval, MaxPollInterval)
	}

	m.cfg.MaxPerMinute = 12
	m.syncPacer()
	if got := m.pacer.PerMinute(); got != 12 {
		t.Errorf("explicit cap = %d, want 12", got)
	}
	m.cfg.MaxPerMinute = 0
	m.interval = 5 * time.Second
	m.syncPacer()
	if got := m.pacer.PerMinute(); got != 36 {
		t.Errorf("derived cap = %d, want 36 for a 5s interval", got)
	}
}

func TestPauseAndResumeKeys(t *testing.T) {
	m := fixture()
	updated, _ := m.Update(keyMsg("p"))
	if !updated.(Model).paused {
		t.Fatal("p did not pause")
	}
	updated, _ = updated.(Model).Update(keyMsg("p"))
	if updated.(Model).paused {
		t.Fatal("p did not resume")
	}
	updated, _ = updated.(Model).Update(keyMsg("+"))
	if updated.(Model).interval != m.interval/2 {
		t.Errorf("+ did not halve the interval: %v", updated.(Model).interval)
	}
}

func TestProbeKeyDisabled(t *testing.T) {
	m := fixture()
	m.cfg.ProbeEnabled = false
	m.showHelp = true
	if view := m.View(); strings.Contains(view, "test request") {
		t.Errorf("disabled probe still advertised: %s", view)
	}
	updated, _ := m.Update(keyMsg("t"))
	if updated.(Model).probing {
		t.Error("t started a probe while probes are disabled")
	}
}

func keyMsg(s string) tea.KeyMsg {
	if s == " " {
		return tea.KeyMsg{Type: tea.KeySpace}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// TestPromptViewFitsWidth checks the key-entry screen obeys the same width
// invariant, including its bordered input box.
func TestPromptViewFitsWidth(t *testing.T) {
	for _, width := range []int{40, 52, 70, 100} {
		m := New(Config{Client: openrouter.New("http://127.0.0.1:9", ""), Interval: time.Second})
		m.width = width
		view := m.View()
		bordered := 0
		for i, line := range strings.Split(view, "\n") {
			if got := lipgloss.Width(line); got > width {
				t.Errorf("width %d: line %d is %d cells wide: %q", width, i, got, line)
			}
			if !strings.ContainsAny(line, "╭│╰") {
				continue
			}
			// Every row of the box must start in the same column.
			if strings.IndexAny(line, "╭│╰") != 2 {
				t.Errorf("width %d: input box row is misaligned: %q", width, line)
			}
			if strings.HasPrefix(line, "  ╭") || strings.HasPrefix(line, "  ╰") {
				bordered++
			}
			if strings.HasPrefix(line, "  │") && !strings.HasSuffix(strings.TrimRight(line, " "), "│") {
				t.Errorf("width %d: input box row lost its right border: %q", width, line)
			}
		}
		if bordered != 2 {
			t.Errorf("width %d: found %d box border rows, want a top and bottom", width, bordered)
		}
	}
}

// TestPreview prints the dashboard for manual inspection with -v.
func TestPreview(t *testing.T) {
	for _, width := range []int{100, 62} {
		m := fixture()
		m.width = width
		t.Logf("width %d\n%s", width, m.View())
	}
	m := New(Config{Client: openrouter.New("http://127.0.0.1:9", ""), Interval: time.Second})
	m.width = 100
	t.Logf("key prompt\n%s", m.View())
}
