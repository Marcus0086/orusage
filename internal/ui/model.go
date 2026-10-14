package ui

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/raghavgupta/orusage/internal/auth"
	"github.com/raghavgupta/orusage/internal/openrouter"
	"github.com/raghavgupta/orusage/internal/pace"
)

const (
	// tickInterval drives clock-driven redraws: countdowns, "updated Ns ago",
	// and the poll schedule check.
	tickInterval = 500 * time.Millisecond
	// burstWindow is how long polling accelerates after observed spend moves.
	burstWindow = 45 * time.Second
	// sampleWindow bounds the history used for burn-rate estimates.
	sampleWindow = 20 * time.Minute
	// maxSamples caps retained history.
	maxSamples = 720
	// flashDuration is how long a transient notice stays on screen.
	flashDuration = 6 * time.Second

	// MinPollInterval and MaxPollInterval bound the adjustable poll rate.
	MinPollInterval = time.Second
	MaxPollInterval = 10 * time.Minute
)

// Mode selects which screen is active.
type Mode int

const (
	// ModeDashboard shows live usage.
	ModeDashboard Mode = iota
	// ModeKeyPrompt asks for an API key.
	ModeKeyPrompt
)

// Config configures a dashboard model.
type Config struct {
	// Client performs the API calls. Its APIKey is set by the key prompt when
	// no key was resolved up front.
	Client *openrouter.Client
	// Key is the resolved credential; an empty Value starts the key prompt.
	Key auth.Key
	// Interval is the base poll interval.
	Interval time.Duration
	// MaxPerMinute caps outgoing requests per minute; zero derives the cap
	// from the poll interval.
	MaxPerMinute int
	// ProbeModel is the model used by the manual test request.
	ProbeModel string
	// ProbeEnabled allows the test-request key.
	ProbeEnabled bool
}

// sample is one observation of spend, kept for burn-rate and history views.
type sample struct {
	At     time.Time
	Spend  float64
	Usage  float64
	Daily  float64
	Budget string
}

// Telemetry counts what the poller has done this session.
type Telemetry struct {
	Requests    int
	Failures    int
	RateLimited int
	Probes      int
	LastStatus  int
	Latencies   []time.Duration
}

// Percentile returns the given percentile of recorded request latencies.
func (t Telemetry) Percentile(p float64) (time.Duration, bool) {
	if len(t.Latencies) == 0 {
		return 0, false
	}
	sorted := append([]time.Duration(nil), t.Latencies...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	idx := int(math.Round(p * float64(len(sorted)-1)))
	return sorted[idx], true
}

// Model is the Bubble Tea model for the usage screen.
type Model struct {
	cfg     Config
	styles  Styles
	palette Palette

	mode  Mode
	input textinput.Model
	key   auth.Key

	info    openrouter.KeyInfo
	hasInfo bool
	obs     openrouter.Observation
	lastErr error
	lastOK  time.Time

	samples   []sample
	telemetry Telemetry

	probe    *openrouter.Probe
	probeObs openrouter.Observation
	probeErr error
	probing  bool

	pacer   *pace.Pacer
	backoff *pace.Backoff

	interval   time.Duration
	nextPoll   time.Time
	lastDelay  time.Duration
	burstUntil time.Time
	inFlight   bool
	paused     bool

	flash      string
	flashUntil time.Time
	showHelp   bool

	now    time.Time
	width  int
	height int
}

// messages
type (
	tickMsg  time.Time
	fetchMsg struct {
		info openrouter.KeyInfo
		obs  openrouter.Observation
		err  error
	}
	probeMsg struct {
		probe openrouter.Probe
		obs   openrouter.Observation
		err   error
	}
)

// New builds the dashboard model.
func New(cfg Config) Model {
	if cfg.Interval <= 0 {
		cfg.Interval = 5 * time.Second
	}
	if cfg.ProbeModel == "" {
		cfg.ProbeModel = "openai/gpt-4o-mini"
	}
	m := Model{
		cfg:      cfg,
		styles:   NewStyles(DefaultPalette()),
		interval: cfg.Interval,
		pacer:    pace.NewPacer(cfg.MaxPerMinute),
		backoff:  pace.NewBackoff(2*time.Second, 2*time.Minute),
		now:      time.Now(),
		width:    100,
	}
	m.input = textinput.New()
	m.input.Placeholder = "sk-or-v1-..."
	m.input.EchoMode = textinput.EchoPassword
	m.input.CharLimit = 256
	m.input.Width = 52

	if key := strings.TrimSpace(cfg.Key.Value); key == "" {
		m.mode = ModeKeyPrompt
		m.input.Focus()
	} else {
		m.mode = ModeDashboard
		m.key = cfg.Key
		m.nextPoll = m.now
	}
	m.syncPacer()
	return m
}

// Init starts the clock; the first poll is scheduled by the dashboard state.
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{tickCmd()}
	if m.mode == ModeKeyPrompt {
		cmds = append(cmds, textinput.Blink)
	}
	return tea.Batch(cmds...)
}

func tickCmd() tea.Cmd {
	return tea.Tick(tickInterval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.input.Width = max(20, min(64, msg.Width-16))
		return m, nil

	case tickMsg:
		m.now = time.Time(msg)
		if m.flash != "" && m.now.After(m.flashUntil) {
			m.flash = ""
		}
		cmds := []tea.Cmd{tickCmd()}
		if m.mode == ModeDashboard && !m.paused && !m.inFlight && !m.now.Before(m.nextPoll) {
			m.inFlight = true
			cmds = append(cmds, m.fetchCmd())
		}
		return m, tea.Batch(cmds...)

	case fetchMsg:
		return m.applyFetch(msg)

	case probeMsg:
		m.probing = false
		m.probe, m.probeObs, m.probeErr = &msg.probe, msg.obs, msg.err
		if msg.err != nil {
			m.probe = nil
			m.setFlash("test request failed: " + msg.err.Error())
		} else {
			m.setFlash(fmt.Sprintf("test request ok · %s via %s · %s · %s",
				msg.probe.Model, msg.probe.Provider, Money(msg.probe.CostUSD), msg.obs.Latency.Round(time.Millisecond)))
		}
		return m, nil

	case tea.KeyMsg:
		if m.mode == ModeKeyPrompt {
			return m.updatePrompt(msg)
		}
		return m.updateDashboard(msg)
	}

	if m.mode == ModeKeyPrompt {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m Model) updatePrompt(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "esc":
		return m, tea.Quit
	case "enter":
		key := normaliseKey(m.input.Value())
		if key == "" {
			m.setFlash("enter an API key first")
			return m, nil
		}
		m.key = auth.Key{Value: key, Source: "entered interactively"}
		m.cfg.Client.APIKey = key
		m.mode = ModeDashboard
		m.nextPoll = m.now
		m.lastErr = nil
		m.backoff.Reset()
		m.setFlash("connected with " + auth.Mask(key))
		return m, nil
	case "ctrl+s":
		key := normaliseKey(m.input.Value())
		if key == "" {
			m.setFlash("nothing to save")
			return m, nil
		}
		path, err := auth.Save(key)
		if err != nil {
			m.setFlash("could not save key: " + err.Error())
			return m, nil
		}
		m.setFlash("saved to " + path)
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	// A pasted key can arrive as one large key message; keep only its
	// credential characters so surrounding shell quoting never sticks.
	if value := m.input.Value(); len([]rune(value)) > 40 {
		if cleaned := normaliseKey(value); cleaned != value {
			m.input.SetValue(cleaned)
		}
	}
	return m, cmd
}

// normaliseKey strips quoting, a Bearer prefix, and whitespace a pasted key
// may carry.
func normaliseKey(raw string) string {
	key := strings.TrimSpace(raw)
	key = strings.Trim(key, `"'`)
	key = strings.TrimSpace(strings.TrimPrefix(key, "Bearer "))
	key = strings.TrimSpace(strings.TrimPrefix(key, "bearer "))
	key = strings.ReplaceAll(key, "\n", "")
	return strings.TrimSpace(key)
}

func (m Model) updateDashboard(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "?":
		m.showHelp = !m.showHelp
		return m, nil
	case "esc":
		m.showHelp = false
		return m, nil
	case "r":
		m.nextPoll = m.now
		m.setFlash("refreshing")
		return m, m.fetchCmd()
	case "p", " ":
		m.paused = !m.paused
		if m.paused {
			m.setFlash("polling paused")
		} else {
			m.nextPoll = m.now
			m.backoff.Reset()
			m.setFlash("polling resumed")
		}
		return m, nil
	case "k":
		m.mode = ModeKeyPrompt
		m.input.SetValue("")
		m.input.Focus()
		return m, textinput.Blink
	case "t":
		if !m.cfg.ProbeEnabled {
			m.setFlash("probe requests are disabled (--no-probe)")
			return m, nil
		}
		if m.probing {
			return m, nil
		}
		m.probing = true
		m.setFlash("sending one-token test request to " + m.cfg.ProbeModel)
		return m, m.probeCmd()
	case "+", "=":
		m.setInterval(m.interval / 2)
		return m, nil
	case "-", "_":
		m.setInterval(m.interval * 2)
		return m, nil
	}
	return m, nil
}

func (m *Model) setInterval(d time.Duration) {
	if d < MinPollInterval {
		d = MinPollInterval
	}
	if d > MaxPollInterval {
		d = MaxPollInterval
	}
	m.interval = d
	m.syncPacer()
	m.nextPoll = m.now
	m.setFlash(fmt.Sprintf("poll interval %s", HumanDuration(d)))
}

func (m *Model) setFlash(text string) {
	m.flash = text
	m.flashUntil = m.now.Add(flashDuration)
}

// syncPacer keeps the request cap consistent with the poll rate: an explicit
// cap is honored, otherwise the cap is derived with headroom for refreshes
// and probes.
func (m *Model) syncPacer() {
	perMinute := m.cfg.MaxPerMinute
	if perMinute <= 0 {
		perMinute = int(math.Ceil(float64(time.Minute) / float64(m.interval)))
		perMinute = max(4, perMinute) * 3
	}
	m.pacer.SetPerMinute(perMinute)
}

func (m Model) fetchCmd() tea.Cmd {
	client := m.cfg.Client
	pacer := m.pacer
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), openrouter.DefaultTimeout+5*time.Second)
		defer cancel()
		if err := pacer.Wait(ctx); err != nil {
			return fetchMsg{err: err}
		}
		info, obs, err := client.Key(ctx)
		return fetchMsg{info: info, obs: obs, err: err}
	}
}

func (m Model) probeCmd() tea.Cmd {
	client := m.cfg.Client
	pacer := m.pacer
	model := m.cfg.ProbeModel
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		if err := pacer.Wait(ctx); err != nil {
			return probeMsg{err: err}
		}
		probe, obs, err := client.Probe(ctx, model)
		return probeMsg{probe: probe, obs: obs, err: err}
	}
}

func (m Model) applyFetch(msg fetchMsg) (tea.Model, tea.Cmd) {
	m.inFlight = false
	m.telemetry.Requests++
	m.telemetry.LastStatus = msg.obs.StatusCode
	if msg.obs.Latency > 0 {
		m.telemetry.Latencies = append(m.telemetry.Latencies, msg.obs.Latency)
		if len(m.telemetry.Latencies) > 200 {
			m.telemetry.Latencies = m.telemetry.Latencies[len(m.telemetry.Latencies)-200:]
		}
	}

	if msg.err != nil {
		m.telemetry.Failures++
		m.lastErr = msg.err
		retryAfter, hasRetryAfter := time.Duration(0), false
		var apiErr *openrouter.APIError
		if errors.As(msg.err, &apiErr) {
			retryAfter, hasRetryAfter = apiErr.RetryAfter, apiErr.HasRetryAfter
			if apiErr.RateLimited() {
				m.telemetry.RateLimited++
			}
		}
		delay := m.backoff.Delay(retryAfter, hasRetryAfter)
		m.lastDelay = delay
		m.pacer.Defer(delay)
		m.nextPoll = m.now.Add(delay)
		switch {
		case openrouter.IsRateLimited(msg.err):
			m.setFlash(fmt.Sprintf("rate limited — retrying in %s", HumanDuration(delay)))
		case errors.Is(msg.err, context.DeadlineExceeded):
			m.setFlash("request timed out — retrying")
		}
		return m, nil
	}

	m.backoff.Reset()
	m.lastDelay = 0
	m.lastErr = nil
	m.lastOK = msg.obs.FetchedAt
	m.obs = msg.obs
	previous := m.info
	hadInfo := m.hasInfo
	m.info = msg.info
	m.hasInfo = true

	if !hadInfo || !sameNumbers(previous, msg.info) {
		m.appendSample(msg.info)
	}
	if hadInfo && m.info.Spend() > previous.Spend() {
		m.burstUntil = m.now.Add(burstWindow)
	}
	m.nextPoll = m.now.Add(m.currentInterval())
	return m, nil
}

// sameNumbers reports whether two payloads agree on every number the screen
// displays, so history only records real movement.
func sameNumbers(a, b openrouter.KeyInfo) bool {
	if a.Usage != b.Usage || a.UsageDaily != b.UsageDaily || a.Spend() != b.Spend() {
		return false
	}
	return true
}

func (m *Model) appendSample(info openrouter.KeyInfo) {
	budget := ""
	if info.EffectiveBudget != nil {
		budget = info.EffectiveBudget.Scope
	}
	m.samples = append(m.samples, sample{
		At:     m.now,
		Spend:  info.Spend(),
		Usage:  info.Usage,
		Daily:  info.UsageDaily,
		Budget: budget,
	})
	if len(m.samples) > maxSamples {
		m.samples = m.samples[len(m.samples)-maxSamples:]
	}
}

// currentInterval is the base interval, accelerated while spend is moving.
func (m Model) currentInterval() time.Duration {
	d := m.interval
	if m.now.Before(m.burstUntil) {
		d = max(MinPollInterval, m.interval/2)
	}
	return max(MinPollInterval, d)
}

// effectiveInterval is what the poller will actually be able to achieve once
// the request cap is taken into account.
func (m Model) effectiveInterval() time.Duration {
	return max(m.currentInterval(), m.pacer.Spacing())
}

// cappedByLimiter reports whether the request cap, not the interval, sets the
// pace.
func (m Model) cappedByLimiter() bool {
	return m.pacer.Spacing() > m.currentInterval()
}

// inBurst reports whether polling is currently accelerated.
func (m Model) inBurst() bool { return m.now.Before(m.burstUntil) }

// remaining returns the budget still available, when the key reports a limit.
func (m Model) remaining() (float64, bool) {
	if limit := m.info.LimitUSD(); limit != nil {
		return max(0, *limit-m.info.Spend()), true
	}
	return 0, false
}

// burnRate estimates spend per hour from the retained samples.
func (m Model) burnRate() (float64, bool) {
	if len(m.samples) < 2 {
		return 0, false
	}
	cutoff := m.now.Add(-sampleWindow)
	first, last := m.samples[0], m.samples[len(m.samples)-1]
	found := false
	for _, s := range m.samples {
		if s.At.Before(cutoff) {
			continue
		}
		if !found {
			first, found = s, true
		}
		last = s
	}
	if !found {
		return 0, false
	}
	elapsed := last.At.Sub(first.At)
	if elapsed < 30*time.Second {
		return 0, false
	}
	return (last.Spend - first.Spend) / elapsed.Hours(), true
}

// sessionSpend is how much the binding budget moved while this screen ran.
func (m Model) sessionSpend() (float64, bool) {
	if len(m.samples) < 2 {
		return 0, false
	}
	return m.samples[len(m.samples)-1].Spend - m.samples[0].Spend, true
}

// historySeries is the sparkline data: remaining budget when a limit exists,
// otherwise lifetime usage.
func (m Model) historySeries() []float64 {
	values := make([]float64, 0, len(m.samples))
	limit := m.info.LimitUSD()
	for _, s := range m.samples {
		if limit != nil {
			values = append(values, max(0, *limit-s.Spend))
		} else {
			values = append(values, s.Usage)
		}
	}
	return values
}

// statusLabel describes the poller in one phrase for the header.
func (m Model) statusLabel() string {
	switch {
	case m.mode == ModeKeyPrompt:
		return "waiting for API key"
	case m.paused:
		return "paused"
	case m.inFlight:
		return "fetching"
	case m.backoff.Attempt() > 0:
		return fmt.Sprintf("retry #%d in %s", m.backoff.Attempt(), HumanDuration(m.nextPoll.Sub(m.now)))
	case !m.lastOK.IsZero():
		return "live · updated " + Ago(m.now.Sub(m.lastOK))
	default:
		return "connecting"
	}
}

// ErrorLine returns a one-line description of the last failure, if any.
func (m Model) ErrorLine() string {
	if m.lastErr == nil {
		return ""
	}
	var apiErr *openrouter.APIError
	if errors.As(m.lastErr, &apiErr) && apiErr.Unauthorized() {
		return "API key rejected (HTTP 401) — press k to enter another"
	}
	return m.lastErr.Error()
}
