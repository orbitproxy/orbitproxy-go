#!/usr/bin/env bash
# 写入 VERSION。ReleaseVersion 由 version.go embed 该文件，发版后再 git tag v<semver>。
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
VERSION="${1:-}"

if [[ ! "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+([.-].*)?$ ]]; then
  echo "usage: $0 <semver>   e.g. $0 0.1.4" >&2
  echo "then:  git tag v${VERSION:-<semver>}" >&2
  exit 1
fi

printf '%s\n' "$VERSION" > "$ROOT/VERSION"
echo "orbitproxy-go VERSION -> $VERSION (tag v$VERSION after commit)"
