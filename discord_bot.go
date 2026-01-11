package main

import (
	"io"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/gordonklaus/portaudio"
	"github.com/joho/godotenv"
	"gopkg.in/hraban/opus.v2"
)

var (
	currentCombinedVC       *discordgo.VoiceConnection
	stopCombinedAudioStream chan struct{}
	currentLogLevel         logLevel = logLevelInfo
)

type logLevel int

const (
	logLevelDebug logLevel = iota
	logLevelInfo
	logLevelWarn
)

func setLogLevelFromEnv() {
	switch strings.ToLower(os.Getenv("LOG_LEVEL")) {
	case "verbose", "debug":
		currentLogLevel = logLevelDebug
	case "warning", "warn":
		currentLogLevel = logLevelWarn
	case "info", "":
		currentLogLevel = logLevelInfo
	default:
		currentLogLevel = logLevelInfo
		log.Printf("WARN: unknown LOG_LEVEL, defaulting to info\n")
	}
}

func logDebugf(format string, a ...interface{}) {
	if currentLogLevel <= logLevelDebug {
		log.Printf("DEBUG: "+format, a...)
	}
}

func logInfof(format string, a ...interface{}) {
	if currentLogLevel <= logLevelInfo {
		log.Printf("INFO: "+format, a...)
	}
}

func logWarnf(format string, a ...interface{}) {
	if currentLogLevel <= logLevelWarn {
		log.Printf("WARN: "+format, a...)
	}
}

func outputFramesFromEnv() int {
	raw := strings.TrimSpace(os.Getenv("OUTPUT_FRAMES"))
	if raw == "" {
		return 1920
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		logWarnf("Invalid OUTPUT_FRAMES=%q, defaulting to 1920", raw)
		return 1920
	}
	return n
}

func main() {
	logFile, err := os.OpenFile("discord_bot.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("Failed to open log file: %v", err)
	}
	defer logFile.Close()

	mw := io.MultiWriter(os.Stdout, logFile)
	log.SetOutput(mw)

	err = godotenv.Load()
	if err != nil {
		logWarnf("Error loading .env file")
		return
	}
	setLogLevelFromEnv()

	token := os.Getenv("BOT_TOKEN")
	if token == "" {
		logWarnf("Bot token not found in .env file")
		return
	}

	voiceChannelName := os.Getenv("VOICE_CHANNEL_NAME")
	if voiceChannelName == "" {
		logWarnf("VOICE_CHANNEL_NAME not found in .env file. Bot will not join a specific channel automatically.")
	}
	targetGuildID := os.Getenv("GUILD_ID")
	voiceChannelID := os.Getenv("VOICE_CHANNEL_ID")

	dg, err := discordgo.New("Bot " + token)
	if err != nil {
		logWarnf("Error creating Discord session: %v", err)
		return
	}
	dg.Identify.Intents = discordgo.IntentsAll

	dg.AddHandler(readyCombined)

	var joined bool
	dg.AddHandler(func(s *discordgo.Session, event *discordgo.GuildCreate) {
		if joined {
			return
		}

		if targetGuildID != "" && event.Guild.ID != targetGuildID {
			return
		}

		if voiceChannelID == "" && voiceChannelName == "" {
			logWarnf("VOICE_CHANNEL_NAME or VOICE_CHANNEL_ID not found in .env file. Bot will not join a voice channel.")
			return
		}

		for _, c := range event.Guild.Channels {
			if c.Type != discordgo.ChannelTypeGuildVoice {
				continue
			}
			if voiceChannelID != "" && c.ID != voiceChannelID {
				continue
			}
			if voiceChannelID == "" && c.Name != voiceChannelName {
				continue
			}

			vc, err := s.ChannelVoiceJoin(event.Guild.ID, c.ID, false, false)
			if err != nil {
				logWarnf("Error joining voice channel: %v", err)
				return
			}
			if voiceChannelID != "" {
				logInfof("Successfully joined voice channel ID '%s'.", c.ID)
			} else {
				logInfof("Successfully joined voice channel '%s' (%s).", voiceChannelName, c.ID)
			}

			joined = true
			currentCombinedVC = vc
			stopCombinedAudioStream = make(chan struct{})
			go streamCombinedAudio(vc, stopCombinedAudioStream)
			return
		}

		if voiceChannelID != "" {
			logWarnf("Voice channel ID '%s' not found in guild '%s'.", voiceChannelID, event.Guild.Name)
			return
		}
		logWarnf("Voice channel '%s' not found in guild '%s'.", voiceChannelName, event.Guild.Name)
	})

	dg.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		if m.Author.ID == s.State.User.ID {
			return
		}

		logDebugf("Received message: %s from %s", m.Content, m.Author.Username)

		if m.Content == "test bot" {
			s.ChannelMessageSend(m.ChannelID, "test diterima")
			logInfof("Replied 'test diterima' to 'test bot' from %s", m.Author.Username)
		}
	})

	err = dg.Open()
	if err != nil {
		logWarnf("Error opening connection: %v", err)
		return
	}
	logInfof("Discord connection opened successfully.")

	logInfof("Discord bot is now running. Press CTRL-C to exit.")
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-sc

	logInfof("Closing Discord session.")

	if stopCombinedAudioStream != nil {
		close(stopCombinedAudioStream)
		time.Sleep(1 * time.Second)
	}

	if currentCombinedVC != nil && currentCombinedVC.Ready {
		logInfof("Disconnecting from voice channel.")
		currentCombinedVC.Disconnect()
	}

	dg.Close()
}

func readyCombined(s *discordgo.Session, event *discordgo.Ready) {
	logInfof("Discord bot is ready!")
	s.UpdateGameStatus(0, "Streaming Audio")
}

func streamCombinedAudio(vc *discordgo.VoiceConnection, stopChan <-chan struct{}) {
	logInfof("Starting audio stream.")
	vc.Speaking(true)
	defer vc.Speaking(false)
	defer logInfof("Audio stream finished.")

	portaudio.Initialize()
	defer portaudio.Terminate()

	in := make([]int16, 960)
	micStream, err := portaudio.OpenDefaultStream(1, 0, 48000, len(in), in)
	if err != nil {
		logWarnf("Error opening PortAudio input stream: %v", err)
		return
	}
	defer micStream.Close()

	outputFrames := outputFramesFromEnv()
	out := make([]int16, outputFrames)
	speakerStream, err := portaudio.OpenDefaultStream(0, 1, 48000, len(out), out)
	if err != nil {
		logWarnf("Error opening PortAudio output stream: %v", err)
		return
	}
	defer speakerStream.Close()

	opusEncoder, err := opus.NewEncoder(48000, 1, opus.AppAudio)
	if err != nil {
		logWarnf("Error creating Opus encoder: %v", err)
		return
	}

	opusDecoder, err := opus.NewDecoder(48000, 1)
	if err != nil {
		logWarnf("Error creating Opus decoder: %v", err)
		return
	}

	go func() {
		decodeBuf := make([]int16, 960)
		pending := make([]int16, 0, outputFrames*2)

		for {
			select {
			case <-stopChan:
				logInfof("Stopping audio receive goroutine.")
				return
			default:
				if vc == nil {
					logWarnf("VC is nil, returning from receive goroutine.")
					return
				}
				if !vc.Ready {
					time.Sleep(200 * time.Millisecond)
					continue
				}
				if vc.OpusRecv == nil {
					time.Sleep(200 * time.Millisecond)
					continue
				}
				p, ok := <-vc.OpusRecv
				if !ok {
					logWarnf("OpusRecv channel closed, returning from receive goroutine.")
					return
				}

				n, err := opusDecoder.Decode(p.Opus, decodeBuf)
				if err != nil {
					logWarnf("Error decoding Opus data: %v", err)
					continue
				}

				pending = append(pending, decodeBuf[:n]...)
				for len(pending) >= outputFrames {
					copy(out, pending[:outputFrames])
					pending = pending[outputFrames:]
					if speakerStream != nil {
						err = speakerStream.Write()
						if err != nil {
							logWarnf("Error writing to PortAudio output stream: %v", err)
						}
					}
				}
			}
		}
	}()

	err = micStream.Start()
	if err != nil {
		logWarnf("Error starting PortAudio input stream: %v", err)
		return
	}
	err = speakerStream.Start()
	if err != nil {
		logWarnf("Error starting PortAudio output stream: %v", err)
		return
	}

	for {
		select {
		case <-stopChan:
			logInfof("Stopping audio send goroutine.")
			micStream.Stop()
			speakerStream.Stop()
			return
		default:
			err = micStream.Read()
			if err != nil {
				continue
			}

			opusData := make([]byte, 1000)
			n, err := opusEncoder.Encode(in, opusData)
			if err != nil {
				logWarnf("Error encoding Opus data: %v", err)
				continue
			}

			if vc.Ready {
				vc.OpusSend <- opusData[:n]
			}
		}
	}
}
