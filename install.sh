#!/bin/sh
# Install the icpt CLI.
#
#   curl -fsSL https://raw.githubusercontent.com/IncepTools/go/main/install.sh | sh
#   curl -fsSL .../install.sh | sh -s -- -b /usr/local/bin -v v1.0.0
#
# Prefer `go install github.com/IncepTools/go/cmd/icpt@latest` if you have a
# Go toolchain. This exists for machines that do not.
set -eu

REPO="IncepTools/go"
BINARY="icpt"
BIN_DIR="${BIN_DIR:-$HOME/.local/bin}"
VERSION="${VERSION:-latest}"

usage() {
	cat <<'USAGE'
Usage: install.sh [-b <dir>] [-v <version>]

  -b <dir>      where to install (default: ~/.local/bin)
  -v <version>  a tag such as v1.0.0 (default: the latest release)
  -h            this message
USAGE
}

while getopts "b:v:h" opt; do
	case "$opt" in
	b) BIN_DIR="$OPTARG" ;;
	v) VERSION="$OPTARG" ;;
	h)
		usage
		exit 0
		;;
	*)
		usage
		exit 1
		;;
	esac
done

die() {
	echo "install.sh: $*" >&2
	exit 1
}

need() { command -v "$1" >/dev/null 2>&1 || die "$1 is required"; }
need uname
need mkdir
need tar

if command -v curl >/dev/null 2>&1; then
	fetch() { curl -fsSL "$1"; }
	download() { curl -fsSL -o "$2" "$1"; }
elif command -v wget >/dev/null 2>&1; then
	fetch() { wget -qO- "$1"; }
	download() { wget -qO "$2" "$1"; }
else
	die "either curl or wget is required"
fi

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$os" in
linux | darwin) ;;
*) die "unsupported operating system: $os (build from source: go install github.com/$REPO/cmd/$BINARY@latest)" ;;
esac

arch="$(uname -m)"
case "$arch" in
x86_64 | amd64) arch="amd64" ;;
arm64 | aarch64) arch="arm64" ;;
*) die "unsupported architecture: $arch" ;;
esac

if [ "$VERSION" = "latest" ]; then
	VERSION="$(fetch "https://api.github.com/repos/$REPO/releases/latest" |
		sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n1)"
	[ -n "$VERSION" ] || die "could not determine the latest release; pass -v <version>"
fi

stripped="${VERSION#v}"
archive="${BINARY}_${stripped}_${os}_${arch}.tar.gz"
url="https://github.com/$REPO/releases/download/$VERSION/$archive"

tmp="$(mktemp -d)"
# shellcheck disable=SC2064
trap "rm -rf '$tmp'" EXIT INT TERM

echo "downloading $BINARY $VERSION ($os/$arch)"
download "$url" "$tmp/$archive" || die "could not download $url"

# Verify the checksum when the manifest is reachable. A failed download that
# installs anyway is worse than no install at all.
if download "https://github.com/$REPO/releases/download/$VERSION/checksums.txt" "$tmp/checksums.txt" 2>/dev/null; then
	if command -v sha256sum >/dev/null 2>&1; then
		want="$(grep " $archive\$" "$tmp/checksums.txt" | awk '{print $1}')"
		got="$(sha256sum "$tmp/$archive" | awk '{print $1}')"
	elif command -v shasum >/dev/null 2>&1; then
		want="$(grep " $archive\$" "$tmp/checksums.txt" | awk '{print $1}')"
		got="$(shasum -a 256 "$tmp/$archive" | awk '{print $1}')"
	fi
	if [ -n "${want:-}" ] && [ "${want:-}" != "${got:-}" ]; then
		die "checksum mismatch for $archive"
	fi
	echo "checksum verified"
else
	echo "warning: could not fetch checksums.txt; skipping verification" >&2
fi

tar -xzf "$tmp/$archive" -C "$tmp"
mkdir -p "$BIN_DIR"
install -m 0755 "$tmp/$BINARY" "$BIN_DIR/$BINARY" 2>/dev/null ||
	{ cp "$tmp/$BINARY" "$BIN_DIR/$BINARY" && chmod 0755 "$BIN_DIR/$BINARY"; }

echo "installed $BIN_DIR/$BINARY"
case ":$PATH:" in
*":$BIN_DIR:"*) ;;
*) echo "note: $BIN_DIR is not on your PATH" >&2 ;;
esac

"$BIN_DIR/$BINARY" version
