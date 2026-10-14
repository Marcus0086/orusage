# Working on orusage

A Go terminal dashboard that polls OpenRouter's `GET /api/v1/key` and renders spend, budgets, and client-side pacing decisions with Bubble Tea. Read [README.md](README.md) for the product behaviour and [INSTALL.md](INSTALL.md) for the distribution paths.

## Build and test

Go comes from the Nix dev shell on this machine; it is not installed globally.

```sh
nix develop                            # or: nix shell nixpkgs#go
make check                             # gofmt, go vet, go test -race
go test ./internal/ui -run TestPreview -v   # render the dashboard at two widths
make build                             # ./orusage with version and commit stamped
```

## Layout

```
main.go                    flags, snapshot text/JSON output, TUI launch
internal/openrouter/       API client: /key, the probe completion, rate-limit parsing
internal/auth/             API key resolution and credential-file parsing
internal/pace/             token-bucket pacer and exponential backoff with jitter
internal/ui/               Bubble Tea model, view, styles, formatting
install.sh                 curl|sh installer (platform detect, sha256 verify)
.goreleaser.yaml           cross-compiled release archives and checksums
```

## Invariants

- **The dashboard fits the terminal.** `TestDashboardFitsWidth` and `TestPromptViewFitsWidth` render every screen at widths 40–118 and fail on any line wider than the terminal. Add a section, add its width case; drop columns by tier instead of letting a row wrap.
- **Gauge thresholds are fixed:** green below 50%, yellow from 50%, orange from 80%, red from 95%. `BandYellowFrom`, `BandOrangeFrom`, `BandRedFrom` own them and `TestGaugeColorBands` pins them.
- **Nothing on screen claims a limit OpenRouter did not publish.** The API returns no `x-ratelimit-*` headers and no `Retry-After` on `/key` or `/chat/completions`, so `Rate limits` says so in words. Rate limiting is enforced client-side by `internal/pace`.
- **Every amount is reachable.** Costs as small as `$0.0000087` print at six decimals rather than rounding to `$0.0000`, and any non-zero budget fraction fills at least one cell of its bar.
- **Sections hide rather than pad.** The History section returns empty until it has a trend or a burn rate; do not reintroduce placeholder rows for absent data.
- **The key never leaves the machine.** It is read from the documented sources in [README.md](README.md#where-the-api-key-comes-from), held in memory, and only persisted on an explicit `ctrl+s`. Never print a full key, log one, or send it anywhere except the configured base URL.

## Don't break the one-liner

`install.sh` reads exactly two artifacts from a release: `orusage_<version>_<os>_<arch>.tar.gz` and `checksums.txt`. Changing the archive `name_template` or the checksum filename in `.goreleaser.yaml` breaks every install line in the README without failing CI at release time. Keep them in sync, or update `install.sh` in the same change.
