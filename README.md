# Discord Go Live Audio Bot

This project is a Discord bot that can receive voice from a channel and play it on the host speaker, and also send microphone audio to the channel. It includes a locally patched `discordgo` copy to handle voice receive decryption reliably.

## Requirements

- Go 1.22+ (uses `go.mod`)
- PortAudio runtime + headers
- Opus library headers

### macOS (Homebrew)

```sh
brew install portaudio opus pkg-config
```

### Raspberry Pi (Debian/Ubuntu)

```sh
sudo apt update
sudo apt install -y portaudio19-dev libopus-dev pkg-config
```

## Setup

1) Copy `.env.example` to `.env` and fill your values:

```sh
cp .env.example .env
```

2) Edit `.env`:

- `BOT_TOKEN`: your Discord bot token
- `GUILD_ID`: server ID
- `VOICE_CHANNEL_NAME`: voice channel name
- `LOG_LEVEL`: `verbose`, `info`, or `warning`
- `OUTPUT_FRAMES`: output buffer size (higher = fewer underflows, more latency)

## Build and Run (macOS/Linux)

```sh
go build -o discord-bot ./discord_bot.go
./discord-bot
```

## Build on Raspberry Pi

Use the build script (recommended on the Pi itself):

```sh
bash scripts/build_rpi.sh
./discord-bot
```

If you need cross-compile from another machine, you must install a suitable ARM toolchain
for CGO (PortAudio). Native build on the Pi is the simplest path.

## Patched discordgo

This repo includes a local copy of `discordgo` at `./discordgo`. The root `go.mod`
contains:

```
replace github.com/bwmarrin/discordgo => ./discordgo
```

So `go build` always uses the patched version.
