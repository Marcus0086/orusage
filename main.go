// Command orusage is a real-time terminal dashboard for OpenRouter usage: it
// polls the key endpoint on a short interval and shows spend against every
// budget, the free-model quota, and the pacing and rate-limit signals observed
// while polling.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/raghavgupta/orusage/internal/auth"
	"github.com/raghavgupta/orusage/internal/openrouter"
	"github.com/raghavgupta/orusage/internal/ui"
)

const version = "0.1.0"

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	flags := flag.NewFlagSet("orusage", flag.ContinueOnError)
	flags.Usage = func() {
		out := flags.Output()
		fmt.Fprintf(out, "orusage %s — real-time OpenRouter usage dashboard\n\n", version)
		fmt.Fprintf(out, "usage: orusage [flags]\n\nflags:\n")
		flags.PrintDefaults()
		fmt.Fprintf(out, "\nkeys: r refresh · p pause · k API key · +/- poll rate · t test request · ? help · q quit\n")
	}

	var (
		apiKey       = flags.String("api-key", "", "OpenRouter API key; overrides every other source")
		baseURL      = flags.String("base-url", openrouter.DefaultBaseURL, "OpenRouter API base URL")
		interval     = flags.Duration("interval", 5*time.Second, "poll interval (1s–10m); the screen halves it briefly after spend moves")
		maxPerMinute = flags.Int("max-req-per-min", 0, "hard cap on outgoing requests per minute; 0 derives it from the poll interval")
		probeModel   = flags.String("probe-model", openrouter.DefaultProbeModel, "model used by the t test request")
		noProbe      = flags.Bool("no-probe", false, "disable the t test-request key")
		once         = flags.Bool("once", false, "print one snapshot as text and exit")
		asJSON       = flags.Bool("json", false, "print the raw key payload and exit")
		showVersion  = flags.Bool("version", false, "print the version and exit")
	)
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *showVersion {
		fmt.Printf("orusage %s\n", version)
		return 0
	}

	key, err := auth.Resolve(*apiKey)
	if err != nil && !*once && !*asJSON {
		// The dashboard can ask for a key interactively, so an unresolved key
		// is only fatal for the non-interactive modes.
		key = auth.Key{}
	}
	if *once || *asJSON {
		if key.Value == "" {
			fmt.Fprintln(os.Stderr, "orusage: no OpenRouter API key found. Searched:")
			for _, path := range auth.SearchPath() {
				fmt.Fprintf(os.Stderr, "  %s\n", path)
			}
			fmt.Fprintln(os.Stderr, "Pass --api-key, or export OPENROUTER_API_KEY.")
			return 2
		}
		return snapshot(key, *baseURL, *asJSON)
	}

	intervalValue := *interval
	if intervalValue < ui.MinPollInterval {
		fmt.Fprintf(os.Stderr, "orusage: --interval %s is below the %s floor; using %s\n", intervalValue, ui.MinPollInterval, ui.MinPollInterval)
		intervalValue = ui.MinPollInterval
	}

	model := ui.New(ui.Config{
		Client:       openrouter.New(*baseURL, key.Value),
		Key:          key,
		Interval:     intervalValue,
		MaxPerMinute: *maxPerMinute,
		ProbeModel:   *probeModel,
		ProbeEnabled: !*noProbe,
	})

	program := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := program.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "orusage: %v\n", err)
		return 1
	}
	return 0
}

// snapshot prints one reading for scripts and smoke checks.
func snapshot(key auth.Key, baseURL string, asJSON bool) int {
	client := openrouter.New(baseURL, key.Value)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	info, obs, err := client.Key(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "orusage: %v\n", err)
		return 1
	}
	if asJSON {
		encoded, err := json.MarshalIndent(info, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "orusage: %v\n", err)
			return 1
		}
		fmt.Println(string(encoded))
		return 0
	}

	lines := [][2]string{
		{"key", auth.Mask(key.Value) + "  (from " + key.Source + ")"},
		{"fetched", obs.FetchedAt.Local().Format("15:04:05") + " in " + obs.Latency.Round(time.Millisecond).String()},
	}
	if budget := info.EffectiveBudget; budget != nil {
		detail := budget.Scope
		if budget.ResetInterval != "" {
			detail += " · resets " + budget.ResetInterval
		}
		if budget.ResetsAt != nil {
			detail += " · " + budget.ResetsAt.Local().Format("2 Jan 15:04") + " (" + ui.In(time.Until(*budget.ResetsAt)) + ")"
		}
		lines = append(lines, [2]string{"binding budget", detail})
	}
	if limit := info.LimitUSD(); limit != nil {
		lines = append(lines,
			[2]string{"limit", fmt.Sprintf("%s of %s used (%s left)", ui.Money(info.Spend()), ui.MoneyFixed(*limit), ui.Money(max(0, *limit-info.Spend())))},
		)
	} else {
		lines = append(lines, [2]string{"limit", "none; " + ui.Money(info.Spend()) + " spent all time"})
	}
	lines = append(lines,
		[2]string{"today", ui.Money(info.UsageDaily)},
		[2]string{"this week", ui.Money(info.UsageWeekly)},
		[2]string{"this month", ui.Money(info.UsageMonthly)},
		[2]string{"all time", ui.Money(info.Usage)},
	)
	if info.FreeModelDailyRequests != nil {
		quota := info.FreeModelDailyRequests
		lines = append(lines, [2]string{"free models", fmt.Sprintf("%d / %d requests today", quota.Used, quota.Limit)})
	}
	lines = append(lines, [2]string{"rate limits", "no x-ratelimit headers returned by the API"})

	for _, line := range lines {
		fmt.Printf("%-16s %s\n", line[0], line[1])
	}

	fmt.Println()
	fmt.Println("budgets:")
	rows := append([]openrouter.Budget(nil), info.Budgets...)
	sort.Slice(rows, func(i, j int) bool { return rows[i].Scope < rows[j].Scope })
	for _, budget := range rows {
		reset := ""
		if budget.ResetsAt != nil {
			reset = " · resets " + ui.In(time.Until(*budget.ResetsAt))
		}
		fmt.Printf("  %-24s %-8s %s of %s (%s)%s\n",
			budget.Scope, budget.ResetInterval,
			ui.Money(budget.SpendUSD), ui.MoneyFixed(budget.LimitUSD),
			strings.TrimSpace(ui.PercentText(budget.SpendUSD, budget.LimitUSD)), reset)
	}
	return 0
}
