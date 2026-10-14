# orusage

A real-time OpenRouter usage dashboard for the terminal, built with [Bubble Tea](https://github.com/charmbracelet/bubbletea).

It polls `GET /api/v1/key` on a short interval and shows what you have spent against every budget OpenRouter reports for your key — the binding budget, each workspace and key guardrail, the free-model request quota — plus the pacing decisions the poller is making so it never trips a rate limit.

```
  orusage · OpenRouter usage                                            ● live · updated 0.4s ago
  key sk-or-v1-4f2...b71 · workspace 3c9d1e07 · paid tier · key expires 8 Oct 2027 · from ~/.env

  Binding budget  workspace · resets daily · 15 Oct 2026 05:30
  ██████░░░░░░░░░░░░░░░░░░░░░░░░░░  0.25%   $0.49 of $200.00 · $199.51 left · resets in 15h 54m

  Budgets
    key_limit                monthly  ███▉░░░░░░░░░░░░░░░░  16.5%   $41.36 of $250.00 · $208.64 left
    workspace                daily    ░░░░░░░░░░░░░░░░░░░░   0.25%  $0.49 of $200.00 · $199.51 left
    workspace_default_key    weekly   ░░░░░░░░░░░░░░░░░░░░   0.32%  $1.60 of $500.00 · $498.40 left
    workspace_default_member weekly   ░░░░░░░░░░░░░░░░░░░░   0.00%  $0.00 of $500.00 · $500.00 left

  Spend
    credits       today        week         month        all time
                  $0.491       $1.56        $41.36       $710.23

  Requests & limits
    Session         42 sent · 0 failed · 0 × 429 · last HTTP 200 · p50 68ms
    Pacing          cap 90/min · poll every 1s · next in 1s · burst
    Rate limits     none published by the API (no x-ratelimit headers)
    Free models     ▎░░░░░░░░░░░░░░░░░░░░░░░░░░░  12 / 1,000 today

  History
    Trend           █▇▇▇▆▆▆▅▅▅▄▄▄▄▃▃▃▂▂▂▁▁▁▁  remaining · over 2m 18s · 24 samples
    Burn rate       $1.56/hr ≈ $37.44/day · $0.06 this session · limit reached in 5d 13h

  ──────────────────────────────────────────────────────────────────────────────────────────────
  r refresh · p pause · ? help · q quit · k API key · +/- poll rate · t test request
```

Gauges run green below 50% of a budget, yellow from 50%, orange from 80%, and red from 95% — staying red once the limit is reached, where the amounts read `$X.00 over` instead of a remaining balance. The thresholds live in `BandYellowFrom`, `BandOrangeFrom`, and `BandRedFrom`, and `TestPaletteProgressesGreenToRed` checks that the four colours really do step through green → yellow → orange → red by hue in both light and dark terminals.

## Quick start

With Nix, everything Go-related comes from a dev shell — Go is never installed globally:

```sh
cd ~/Documents/raghav_stuff/orusage
nix develop          # or: nix shell nixpkgs#go
go run .
```

Or build a binary:

```sh
nix build .#default && ./result/bin/orusage
# or, inside the shell
go build -o orusage . && ./orusage
```

Without Nix, any Go 1.24+ toolchain works: `go run .`.

## Where the API key comes from

`orusage` resolves the key itself, first match wins:

1. `--api-key`
2. `$OPENROUTER_API_KEY`
3. `.env` in the working directory, then each parent directory up to `$HOME` (so running from inside a project picks up that project's `.env`)
4. `~/.env`
5. `~/.config/orusage/credentials`
6. `~/.dsh/.credentials.yaml` (the DeepSeek Harness credential store)
7. `~/.config/openrouter/credentials`, then `~/.openrouter/credentials`

Credentials files may use `KEY=value`, YAML (`OPENROUTER_API_KEY: sk-or-…`), or JSON (`{"api_key": "…"}`).

If nothing is found, the dashboard opens on a key prompt. The key is held in memory only; press `ctrl+s` to persist it to `~/.config/orusage/credentials` with `0600` permissions.

## Keys

| Key | Action |
| --- | --- |
| `r` | poll immediately |
| `p` / `space` | pause or resume polling |
| `+` / `-` | halve or double the poll interval (1s – 10m) |
| `k` | enter a different API key |
| `t` | send one minimal completion to prove the key can spend |
| `?` | help |
| `q` / `ctrl+c` | quit |

## Flags

```
--api-key string          OpenRouter API key; overrides every other source
--base-url string         OpenRouter API base URL (default https://openrouter.ai/api/v1)
--interval duration       poll interval (default 5s)
--max-req-per-min int     hard cap on outgoing requests per minute (default 0 = derive from the interval)
--probe-model string      model used by the t test request (default openai/gpt-6-luna)
--no-probe                disable the t test-request key
--once                    print one snapshot as text and exit
--json                    print the raw key payload and exit
--version                 print the version and exit
```

`--once` and `--json` are scriptable and skip the TUI entirely; with no key resolved they exit `2` and list every location they searched.

## How polling and rate limits are handled

OpenRouter returns **no** `x-ratelimit-*` or `Retry-After` headers on either `/key` or `/chat/completions` (verified against the live API), and the `rate_limit` object in the key payload is documented as deprecated. So `orusage` treats rate limiting as a client-side responsibility and reports exactly what it did:

- **Paced requests.** A token-bucket pacer spaces requests according to a per-minute budget. By default the budget is derived from the poll interval with 3× headroom (at least 12/min); `--max-req-per-min` sets an explicit hard cap, and the screen says `held back by request cap` when the cap — not the interval — sets the pace.
- **Backoff with jitter.** On failure the interval grows exponentially (2s → 2m, ±15% jitter). A `429` increments the visible `× 429` counter, and a server-provided `Retry-After` — should OpenRouter start sending one — is honored exactly and also flows through the generic `x-ratelimit-*` parser.
- **Burst on activity.** When observed spend moves between two polls, polling halves its interval for 45 seconds so the numbers feel live while requests are actually happening, then relaxes again. The header shows `burst` while that is active.
- **Honest reporting.** The `Rate limits` row states that no headers were returned rather than implying a limit that was never observed. The free-model daily quota (`free_model_daily_requests`) is shown as a bar because it is a real, published quota.
- **The `t` probe** (disabled with `--no-probe`) sends a single `max_tokens: 1` completion, records its cost, provider, and latency, and goes through the same pacer as the poller. The default model is `openai/gpt-6-luna`, a reasoning model, so an answer can take 5–20 seconds and costs a few micro-dollars — `orusage` prints such amounts at six decimals rather than rounding them to `$0.0000`.

## Development

```sh
nix develop
go test ./...          # unit tests for key resolution, the API client, pacing, and layout
go vet ./...
go test ./internal/ui -run TestPreview -v   # print the dashboard at two widths
```

Layout is a tested invariant: `TestDashboardFitsWidth` and `TestPromptViewFitsWidth` render every screen at widths from 40 to 118 columns and fail if any line exceeds the terminal, so sections shed columns instead of wrapping. `TestGaugeColorBands` pins the gauge thresholds and `TestPaletteProgressesGreenToRed` verifies the four gauge colours by hue angle.
