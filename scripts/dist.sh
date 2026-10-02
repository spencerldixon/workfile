#!/bin/sh
# Build self-contained archives. Run from the repository root.
set -eu

version=${1:-dev}
case "$version" in
  ''|*[!A-Za-z0-9._-]*) echo 'Version may contain letters, numbers, dots, underscores, and hyphens.' >&2; exit 2 ;;
esac

go_command=${GO:-go}
destination="dist/$version"
mkdir -p "$destination"
stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT HUP INT TERM

for target in darwin/arm64 darwin/amd64 linux/arm64 linux/amd64 windows/amd64 windows/arm64; do
  platform=${target%/*}
  architecture=${target#*/}
  name="workfile_${version}_${platform}_${architecture}"
  package="$stage/$name"
  mkdir -p "$package"
  binary=wf
  if [ "$platform" = windows ]; then binary=wf.exe; fi
  CGO_ENABLED=0 GOOS="$platform" GOARCH="$architecture" "$go_command" build \
    -trimpath -ldflags "-s -w -X main.version=$version" -o "$package/$binary" ./cmd/wf
  cp README.md THIRD_PARTY_NOTICES.md "$package/"
  cp -R docs example "$package/"
  LC_ALL=C tar -czf "$destination/$name.tar.gz" -C "$stage" "$name"
  echo "$destination/$name.tar.gz"
done

(cd "$destination" && LC_ALL=C shasum -a 256 ./*.tar.gz > SHA256SUMS)
