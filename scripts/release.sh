#!/usr/bin/env bash
# Builds the release archives for a version into release/: one tar.gz per
# platform (a zip for Windows) with the web UI embedded, and a SHA256SUMS file.
#
#   scripts/release.sh v0.1.0
#
# The binaries are pure Go (no cgo), so every platform builds from one machine.
set -euo pipefail

version="${1:-}"
if [[ ! "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$ ]]; then
  echo "usage: scripts/release.sh vMAJOR.MINOR.PATCH" >&2
  exit 2
fi

cd "$(dirname "$0")/.."
out=release
rm -rf "$out"
mkdir -p "$out"

# The UI is embedded in the binary, so it is built first, once.
make web

platforms=("darwin arm64" "darwin amd64" "linux amd64" "linux arm64" "windows amd64" "windows arm64")
for p in "${platforms[@]}"; do
  read -r goos goarch <<<"$p"
  name="waypoint_${version}_${goos}_${goarch}"
  stage="$out/$name"
  mkdir -p "$stage"
  exe=waypoint
  [[ "$goos" == windows ]] && exe=waypoint.exe
  echo "building $name"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build -trimpath \
    -ldflags "-s -w -X waypoint/internal/cli.Version=${version}" \
    -o "$stage/$exe" ./cmd/waypoint
  cp README.md "$stage/"
  if [[ "$goos" == windows ]]; then
    (cd "$out" && zip -qr "$name.zip" "$name") # a zip opens everywhere on Windows
  else
    tar -C "$out" -czf "$out/$name.tar.gz" "$name"
  fi
  rm -rf "$stage"
done

(cd "$out" && shasum -a 256 ./*.tar.gz ./*.zip | sed 's# \./# #' > SHA256SUMS)
echo
cat "$out/SHA256SUMS"
