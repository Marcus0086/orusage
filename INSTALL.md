# Installing orusage

Three ways, pick one. Everything ends with the same binary on your `PATH` and the same first-run behaviour.

| You are… | Do this |
| --- | --- |
| On macOS or Linux, want it now | [One-liner](#one-liner) |
| Pasted this repo into Claude, Codex, or Cursor | [Give the agent this prompt](#give-the-agent-this-prompt) |
| Already have Go 1.24+ | [`go install`](#go-install) |
| Already use Nix | [`nix run`](#nix) |
| Want a build from a checkout you own | [From source](#from-source) |

## One-liner

```sh
curl -fsSL https://raw.githubusercontent.com/Marcus0086/orusage/main/install.sh | sh
```

The script downloads the release archive for your OS and architecture, verifies it against `checksums.txt`, and installs to `~/.local/bin/orusage`. It uses `curl` or `wget`, whichever exists, and never needs `sudo`.

Options (flags or environment variables):

```sh
# somewhere else on your PATH
curl -fsSL .../install.sh | sh -s -- --bin-dir /usr/local/bin

# pin a version
curl -fsSL .../install.sh | sh -s -- --version v0.1.0

# a fork, or a private mirror
ORUSAGE_REPO=you/orusage curl -fsSL .../install.sh | sh

# build from source instead of downloading (needs Go 1.24+)
curl -fsSL .../install.sh | sh -s -- --source
```

Run `install.sh --help` for the full list, or `--dry-run` to see every command it would run without touching anything.

If the installer reports that `~/.local/bin` is not on your `PATH`, it prints the one line to add to `~/.zshrc`, `~/.bashrc`, or `~/.config/fish/config.fish`.

## Give the agent this prompt

This is the part that makes it easy to hand to someone else. Paste the whole block into a fresh Claude, Codex, or Cursor session — no cloning, no reading the repo first.

```text
Install `orusage`, a terminal dashboard for OpenRouter spend and rate limits:
https://github.com/Marcus0086/orusage

Do this in order, and stop at the first step that works:

1. Detect my OS and CPU architecture (uname -s, uname -m).
2. Prefer the official installer:
   curl -fsSL https://raw.githubusercontent.com/Marcus0086/orusage/main/install.sh | sh
   It installs to ~/.local/bin. If that directory is not on my PATH, add it to my
   shell profile (~/.zshrc, ~/.bashrc, or ~/.config/fish/config.fish — use whichever
   shell I actually run) and tell me to open a new shell.
3. If the installer fails and Go 1.24+ is available, build from source instead:
   go install github.com/Marcus0086/orusage@latest
   and make sure $(go env GOPATH)/bin is on my PATH.
4. If neither works, clone into ~/.local/share/orusage, run `go build -o orusage .`,
   and symlink the binary into ~/.local/bin. Do not run the build as root.

Then verify, and show me the real output of each check:
   orusage --version
   orusage --once

`orusage --once` prints one snapshot as text and exits. It needs an OpenRouter API
key. It looks in this order, first match wins:
   --api-key, $OPENROUTER_API_KEY, .env here or in any parent directory up to $HOME,
   ~/.env, ~/.config/orusage/credentials, ~/.dsh/.credentials.yaml,
   ~/.config/openrouter/credentials, ~/.openrouter/credentials.

If it exits with code 2 it found no key and prints every path it searched — do not
guess or invent a key. Tell me which of these I should do, and wait for my answer:
   a) I set OPENROUTER_API_KEY myself, or
   b) I put OPENROUTER_API_KEY=sk-or-... in ~/.env (chmod 600), or
   c) I paste a key for you to save with `orusage` and ctrl+s.

Never print a full API key back to me, never commit one, and never send the key
anywhere except https://openrouter.ai. Once a key resolves, launch the dashboard
with plain `orusage` so I can see it live; leave it running and tell me the keys:
r refresh, p pause, +/- poll rate, t test request, ? help, q quit.

Finally tell me: the installed version, the install path, which credential source
was used, and any step you had to substitute.
```

Why the prompt is written this way: the three-step install order covers the common cases without the agent improvising, the credential order matches what the binary actually does so the agent does not go hunting for a config file that does not exist, and the explicit "do not invent a key" clause stops an agent from inventing a placeholder that fails confusingly later.

## go install

```sh
go install github.com/Marcus0086/orusage@latest
```

Needs Go 1.24 or newer; the binary lands in `$(go env GOPATH)/bin`. The module path is the repository URL, so this works only while the repository is public.

## Nix

```sh
nix run github:Marcus0086/orusage            # run without installing
nix profile install github:Marcus0086/orusage
```

Or, from a checkout:

```sh
nix build .#default && ./result/bin/orusage
nix develop                                    # dev shell with Go, gopls, gotools
```

## From source

```sh
git clone https://github.com/Marcus0086/orusage
cd orusage
make build        # -> ./orusage, with version and commit injected
make install      # -> ~/.local/bin/orusage
make check        # gofmt, go vet, go test -race
```

A plain `go build -o orusage .` works too; `make` only adds the `-ldflags` version stamp.

## Where the API key comes from

`orusage` resolves the key itself, first match wins:

1. `--api-key`
2. `$OPENROUTER_API_KEY`
3. `.env` in the working directory, then each parent directory up to `$HOME`
4. `~/.env`
5. `~/.config/orusage/credentials`
6. `~/.dsh/.credentials.yaml`
7. `~/.config/openrouter/credentials`, then `~/.openrouter/credentials`

Credential files may be `KEY=value`, YAML (`OPENROUTER_API_KEY: sk-or-…`), or JSON (`{"api_key": "…"}`). The cheapest setup for one machine:

```sh
printf 'OPENROUTER_API_KEY=sk-or-...\n' >> ~/.env && chmod 600 ~/.env
```

With no key anywhere, the dashboard opens on a key prompt; the key stays in memory unless you press `ctrl+s`, which writes `~/.config/orusage/credentials` at mode `0600`.

## Verify an install

```sh
orusage --version               # orusage 0.1.0 (abcdef1)
orusage --once                  # one snapshot as text, then exit
orusage --once --json           # the raw /api/v1/key payload
```

`--once` and `--json` skip the TUI, which makes them the right check for a script or an agent. Both exit `2` and list the searched paths when no key resolves.

## Uninstall

```sh
rm -f ~/.local/bin/orusage ~/.config/orusage/credentials
```

## Releasing (maintainers)

Tag and push; GitHub Actions does the rest.

```sh
git tag -a v0.1.0 -m "v0.1.0" && git push origin v0.1.0
```

`.github/workflows/release.yml` runs the tests and then goreleaser, which cross-compiles darwin and linux on amd64 and arm64, publishes `orusage_<version>_<os>_<arch>.tar.gz` plus `checksums.txt`, and attaches them to the GitHub release. The installer reads exactly those two artifacts, so a release that skips goreleaser breaks the one-liner.

Dry-run a release locally, with no tag and no upload:

```sh
goreleaser release --snapshot --clean     # or: make snapshot
```

`install.sh` does not shell out to goreleaser's installers: it downloads the archive directly, so nothing extra is published or required. The `--source` fallback builds with `go build` from a clone, which is the path that works when no release exists yet.

A Homebrew tap is optional and commented out in `.goreleaser.yaml`; fill in the tap repository name and uncomment the `brews:` block to publish a formula on each release.
