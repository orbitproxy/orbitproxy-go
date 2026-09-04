#!/usr/bin/env bash
# 把当前工作区源码和 VERSION 打成远端模块：commit + tag + push。
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
VERSION="${1:-}"
shift || true
NOTES="${*:-}"
TAG_MESSAGE="${NOTES:-release ${VERSION}}"

if [[ ! "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+([.-].*)?$ ]]; then
  echo "usage: $0 <semver> [更新说明]   e.g. $0 0.3.2 'find installed MCP packages'" >&2
  exit 1
fi

cd "$ROOT"

"$ROOT/scripts/bump-version.sh" "$VERSION"

git add -u
git add -A -- . ':!*.exe'

if ! git diff --cached --name-only | grep -qvE '^VERSION$'; then
  echo "error: only VERSION is staged; uncommitted SDK source is missing" >&2
  git reset -q HEAD -- VERSION || true
  exit 1
fi

if ! git diff --cached --name-only | grep -qx 'appdir/appdir.go'; then
  if [[ -f appdir/appdir.go ]] && ! git cat-file -e "HEAD:appdir/appdir.go" 2>/dev/null; then
    echo "error: appdir is not staged; refuse empty SDK tag" >&2
    exit 1
  fi
fi

if git rev-parse "v${VERSION}" >/dev/null 2>&1; then
  echo "error: tag v${VERSION} already exists" >&2
  exit 1
fi

git commit -m "release ${VERSION}"
git tag -a "v${VERSION}" -m "${TAG_MESSAGE}"
git push origin HEAD
git push origin "v${VERSION}"

echo "published github.com/orbitproxy/orbitproxy-go@v${VERSION}"
