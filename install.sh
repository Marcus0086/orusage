#!/bin/sh
# orusage installer: download the release binary for this platform, verify its
# checksum, and install it on the PATH.
#
#   curl -fsSL https://raw.githubusercontent.com/Marcus0086/orusage/main/install.sh | sh
#
# Options (each also has an environment variable):
#   --repo OWNER/NAME   repository to install from          (ORUSAGE_REPO)
#   --version vX.Y.Z    release to install, default latest  (ORUSAGE_VERSION)
#   --bin-dir DIR       install location, default ~/.local/bin (ORUSAGE_BIN_DIR)
#   --source            build from source with git and Go instead of downloading
#   --dry-run           print the plan and exit without changing anything
#   --no-verify         skip checksum verification
#   --help
set -eu

REPO="${ORUSAGE_REPO:-Marcus0086/orusage}"
VERSION="${ORUSAGE_VERSION:-}"
BIN_DIR="${ORUSAGE_BIN_DIR:-$HOME/.local/bin}"
FROM_SOURCE=0
DRY_RUN=0
VERIFY=1

usage() {
	cat <<'EOF'
Install orusage, a real-time OpenRouter usage dashboard for the terminal.

  curl -fsSL https://raw.githubusercontent.com/Marcus0086/orusage/main/install.sh | sh

Options:
  --repo OWNER/NAME   repository to install from (default Marcus0086/orusage)
  --version vX.Y.Z    release to install (default: the latest release)
  --bin-dir DIR       install location (default: ~/.local/bin)
  --source            build from source with git and Go instead of downloading
  --dry-run           print the plan and exit without changing anything
  --no-verify         skip checksum verification
  --help              show this message
EOF
}

log() { printf 'orusage: %s\n' "$*" >&2; }
die() {
	printf 'orusage: %s\n' "$*" >&2
	exit 1
}

while [ $# -gt 0 ]; do
	case "$1" in
	--repo)
		REPO="${2:?--repo needs OWNER/NAME}"
		shift 2
		;;
	--version)
		VERSION="${2:?--version needs a tag such as v0.1.0}"
		shift 2
		;;
	--bin-dir)
		BIN_DIR="${2:?--bin-dir needs a directory}"
		shift 2
		;;
	--source)
		FROM_SOURCE=1
		shift
		;;
	--dry-run)
		DRY_RUN=1
		shift
		;;
	--no-verify)
		VERIFY=0
		shift
		;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		log "unknown option: $1"
		usage >&2
		exit 2
		;;
	esac
done

if command -v curl >/dev/null 2>&1; then
	fetch() { curl -fsSL "$1"; }
	fetch_to() { curl -fsSL -o "$2" "$1"; }
elif command -v wget >/dev/null 2>&1; then
	fetch() { wget -qO- "$1"; }
	fetch_to() { wget -qO "$2" "$1"; }
else
	die "neither curl nor wget is available; install one, or build from source with --source"
fi

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
darwin | linux) ;;
*) die "no prebuilt binary for $os; re-run with --source to build from source" ;;
esac

arch=$(uname -m)
case "$arch" in
x86_64 | amd64) arch=amd64 ;;
arm64 | aarch64) arch=arm64 ;;
*) die "no prebuilt binary for $arch; re-run with --source to build from source" ;;
esac

if [ "$FROM_SOURCE" = 0 ] && [ -z "$VERSION" ]; then
	VERSION=$(fetch "https://api.github.com/repos/$REPO/releases/latest" 2>/dev/null |
		sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -1) || VERSION=""
fi

target="$BIN_DIR/orusage"

install_binary() {
	source_binary="$1"
	if [ "$DRY_RUN" = 1 ]; then
		log "would install $source_binary to $target"
		return 0
	fi
	mkdir -p "$BIN_DIR"
	install -m 0755 "$source_binary" "$target" || die "could not write $target"
	log "installed $target"
	if installed=$("$target" --version 2>/dev/null); then
		log "verified: $installed"
	else
		die "$target did not run; please report this"
	fi
}

path_hint() {
	case ":${PATH:-}:" in
	*":$BIN_DIR:"*) ;;
	*)
		shell_name=$(basename "${SHELL:-sh}")
		case "$shell_name" in
		fish) rc="~/.config/fish/config.fish"; line="fish_add_path $BIN_DIR" ;;
		bash) rc="~/.bashrc"; line="export PATH=\"$BIN_DIR:\$PATH\"" ;;
		*) rc="~/.zshrc"; line="export PATH=\"$BIN_DIR:\$PATH\"" ;;
		esac
		log ""
		log "$BIN_DIR is not on your PATH. Add it with:"
		log "  echo '$line' >> $rc"
		log "then open a new shell."
		;;
	esac
}

build_from_source() {
	command -v go >/dev/null 2>&1 || die "building from source needs Go 1.24 or newer (https://go.dev/dl)"
	command -v git >/dev/null 2>&1 || die "building from source needs git"
	tmp=$(mktemp -d)
	trap 'rm -rf "$tmp"' EXIT
	log "cloning https://github.com/$REPO"
	git clone --depth 1 "https://github.com/$REPO" "$tmp/src" >/dev/null 2>&1 ||
		die "could not clone https://github.com/$REPO"
	[ -f "$tmp/src/go.mod" ] ||
		die "cloned $REPO but it has no go.mod; the repository is empty or is not orusage"
	log "building with $(go version)"
	(cd "$tmp/src" && go build -ldflags "-s -w" -o "$tmp/orusage" .) || die "go build failed"
	install_binary "$tmp/orusage"
}

if [ "$FROM_SOURCE" = 1 ] || [ -z "$VERSION" ]; then
	[ "$FROM_SOURCE" = 1 ] || log "no published release found for $REPO; building from source instead"
	if [ "$DRY_RUN" = 1 ]; then
		log "plan: clone https://github.com/$REPO, build with Go, install to $target"
		exit 0
	fi
	build_from_source
else
	version_number=${VERSION#v}
	asset="orusage_${version_number}_${os}_${arch}.tar.gz"
	base="https://github.com/$REPO/releases/download/$VERSION"
	log "installing orusage $VERSION for $os/$arch"
	log "  $base/$asset"

	if [ "$DRY_RUN" = 1 ]; then
		log "would download $asset, verify it, and install to $target"
		exit 0
	fi

	tmp=$(mktemp -d)
	trap 'rm -rf "$tmp"' EXIT
	fetch_to "$base/$asset" "$tmp/$asset" || die "download failed: $base/$asset"

	if [ "$VERIFY" = 1 ]; then
		if command -v shasum >/dev/null 2>&1; then
			sha() { shasum -a 256 "$1" | awk '{print $1}'; }
		elif command -v sha256sum >/dev/null 2>&1; then
			sha() { sha256sum "$1" | awk '{print $1}'; }
		else
			die "no shasum or sha256sum available to verify the download; re-run with --no-verify"
		fi
		want=$(fetch "$base/checksums.txt" | awk -v a="$asset" '$2 == a || $2 == "*" a { print $1 }' | head -1)
		[ -n "$want" ] || die "the release publishes no checksum for $asset"
		got=$(sha "$tmp/$asset")
		[ "$want" = "$got" ] || die "checksum mismatch for $asset (expected $want, got $got)"
		log "checksum verified"
	fi

	tar -xzf "$tmp/$asset" -C "$tmp" || die "could not extract $asset"
	[ -f "$tmp/orusage" ] || die "$asset did not contain an orusage binary"
	install_binary "$tmp/orusage"
fi

log ""
log "Get an API key from https://openrouter.ai/keys, then run:"
log "  orusage                 # shows a prompt if no key is stored yet"
log "  orusage --once          # one-shot snapshot, good for scripts"
path_hint
