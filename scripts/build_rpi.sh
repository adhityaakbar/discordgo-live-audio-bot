#!/usr/bin/env bash
set -euo pipefail

arch="$(uname -m)"
goarch=""
goarm=""

case "$arch" in
  aarch64)
    goarch="arm64"
    ;;
  armv7l)
    goarch="arm"
    goarm="7"
    ;;
  armv6l)
    goarch="arm"
    goarm="6"
    ;;
  *)
    echo "Unsupported arch for Raspberry Pi build: ${arch}" >&2
    exit 1
    ;;
esac

echo "Building for linux/${goarch} (GOARM=${goarm:-})"
if [[ -n "$goarm" ]]; then
  env GOOS=linux GOARCH="$goarch" GOARM="$goarm" CGO_ENABLED=1 \
    go build -o discord-bot ./discord_bot.go
else
  env GOOS=linux GOARCH="$goarch" CGO_ENABLED=1 \
    go build -o discord-bot ./discord_bot.go
fi
