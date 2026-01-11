#!/usr/bin/env bash
set -euo pipefail

ensure_deps() {
  if ! command -v pkg-config >/dev/null 2>&1; then
    sudo apt update
    sudo apt install -y pkg-config
  fi

  if ! pkg-config --exists portaudio-2.0; then
    sudo apt update
    sudo apt install -y portaudio19-dev
  fi

  if ! pkg-config --exists opus; then
    sudo apt update
    sudo apt install -y libopus-dev
  fi

  if ! pkg-config --exists opusfile; then
    sudo apt update
    sudo apt install -y libopusfile-dev
  fi
}

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

ensure_deps

echo "Building for linux/${goarch} (GOARM=${goarm:-})"
if [[ -n "$goarm" ]]; then
  env GOOS=linux GOARCH="$goarch" GOARM="$goarm" CGO_ENABLED=1 \
    go build -o discord-bot ./discord_bot.go
else
  env GOOS=linux GOARCH="$goarch" CGO_ENABLED=1 \
    go build -o discord-bot ./discord_bot.go
fi
