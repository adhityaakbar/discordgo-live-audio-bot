package main

import (
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/gordonklaus/portaudio"
	"github.com/joho/godotenv"
	"gopkg.in/hraban/opus.v2"
)

var (
	currentReceiverVC       *discordgo.VoiceConnection
	stopReceiverAudioStream chan struct{}
)

func main() {
	// Open a file for logging
	logFile, err := os.OpenFile("receiver_bot.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("Failed to open log file: %v", err)
	}
	defer logFile.Close()

	// Set log output to the file and stdout
	mw := io.MultiWriter(os.Stdout, logFile)
	log.SetOutput(mw)

	// Load the .env file
	err = godotenv.Load()
	if err != nil {
		log.Println("Error loading .env file")
		return
	}

	// Get the bot token from the .env file
	token := os.Getenv("BOT_TOKEN")
	if token == "" {
		log.Println("Bot token not found in .env file")
		return
	}

	voiceChannelName := os.Getenv("VOICE_CHANNEL_NAME")
	if voiceChannelName == "" {
		log.Println("VOICE_CHANNEL_NAME not found in .env file. Bot will not join a specific channel automatically.")
	}
	targetGuildID := os.Getenv("GUILD_ID")
	voiceChannelID := os.Getenv("VOICE_CHANNEL_ID")

	// Create a new Discord session
	dg, err := discordgo.New("Bot " + token)
	if err != nil {
		log.Println("Error creating Discord session:", err)
		return
	}
	dg.Identify.Intents = discordgo.IntentsAll
	dg.LogLevel = discordgo.LogDebug
	discordgo.Logger = func(msgL, caller int, format string, a ...interface{}) {
		log.Printf("discordgo: "+format, a...)
	}

	// Add a handler for the ready event
	dg.AddHandler(readyReceiver)

	// Add a handler for GuildCreate to auto-join the voice channel
	var joined bool
	dg.AddHandler(func(s *discordgo.Session, event *discordgo.GuildCreate) {
		if joined {
			return
		}

		if targetGuildID != "" && event.Guild.ID != targetGuildID {
			return
		}

		if voiceChannelID == "" && voiceChannelName == "" {
			log.Println("VOICE_CHANNEL_NAME or VOICE_CHANNEL_ID not found in .env file. Bot will not join a voice channel.")
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

			s.Lock()
			if _, ok := s.VoiceConnections[event.Guild.ID]; !ok {
				s.VoiceConnections[event.Guild.ID] = &discordgo.VoiceConnection{LogLevel: discordgo.LogDebug}
			}
			s.Unlock()

			vc, err := s.ChannelVoiceJoin(event.Guild.ID, c.ID, false, false)
			if err != nil {
				log.Printf("Error joining voice channel: %v\n", err)
				return
			}
			if voiceChannelID != "" {
				log.Printf("Successfully joined voice channel ID '%s'.\n", c.ID)
			} else {
				log.Printf("Successfully joined voice channel '%s' (%s).\n", voiceChannelName, c.ID)
			}
			vc.LogLevel = discordgo.LogDebug
			vc.AddHandler(func(v *discordgo.VoiceConnection, vs *discordgo.VoiceSpeakingUpdate) {
				log.Printf("VoiceSpeakingUpdate: user=%s speaking=%t ssrc=%d\n", vs.UserID, vs.Speaking, vs.SSRC)
			})
			go func() {
				for i := 0; i < 10; i++ {
					log.Printf("VC state: ready=%t opusRecvNil=%t\n", vc.Ready, vc.OpusRecv == nil)
					time.Sleep(1 * time.Second)
				}
			}()

			joined = true
			currentReceiverVC = vc // Assign to global variable
			stopReceiverAudioStream = make(chan struct{}) // Initialize the channel
			go receiveAudio(s, vc, stopReceiverAudioStream)
			return
		}

		if voiceChannelID != "" {
			log.Printf("Voice channel ID '%s' not found in guild '%s'.\n", voiceChannelID, event.Guild.Name)
			return
		}
		log.Printf("Voice channel '%s' not found in guild '%s'.\n", voiceChannelName, event.Guild.Name)
	})

	dg.AddHandler(func(s *discordgo.Session, vsu *discordgo.VoiceStateUpdate) {
		if vsu == nil || vsu.VoiceState == nil {
			return
		}
		if targetGuildID != "" && vsu.GuildID != targetGuildID {
			return
		}
		if voiceChannelID != "" && vsu.ChannelID != voiceChannelID {
			return
		}
		log.Printf(
			"VoiceStateUpdate: user=%s channel=%s deaf=%t mute=%t self_deaf=%t self_mute=%t suppress=%t\n",
			vsu.UserID, vsu.ChannelID, vsu.Deaf, vsu.Mute, vsu.SelfDeaf, vsu.SelfMute, vsu.Suppress,
		)
	})

	dg.AddHandler(func(s *discordgo.Session, vsu *discordgo.VoiceServerUpdate) {
		if vsu == nil {
			return
		}
		if targetGuildID != "" && vsu.GuildID != targetGuildID {
			return
		}
		log.Printf("VoiceServerUpdate: guild=%s endpoint=%s token_set=%t\n", vsu.GuildID, vsu.Endpoint, vsu.Token != "")
	})

	// Open a websocket connection to Discord
	err = dg.Open()
	if err != nil {
		log.Println("Error opening connection:", err)
		return
	}
	log.Println("Discord connection opened successfully.")

	// Wait here until CTRL-C or other term signal is received.
	log.Println("Receiver Bot is now running. Press CTRL-C to exit.")
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-sc

	// Cleanly close down the Discord session.
	log.Println("Closing Discord session for Receiver Bot.")

	// Signal receiveAudio goroutine to stop and clean up voice connection
	if stopReceiverAudioStream != nil {
		close(stopReceiverAudioStream)
		// Give some time for the goroutine to clean up
		time.Sleep(1 * time.Second)
	}

	if currentReceiverVC != nil && currentReceiverVC.Ready {
		log.Println("Disconnecting from voice channel for Receiver Bot.")
		currentReceiverVC.Disconnect()
	}

	dg.Close()
}

func readyReceiver(s *discordgo.Session, event *discordgo.Ready) {
	log.Println("Receiver Bot is ready!")
	s.UpdateGameStatus(0, "Receiving Audio")
}

func receiveAudio(s *discordgo.Session, vc *discordgo.VoiceConnection, stopChan <-chan struct{}) {
	log.Println("Starting audio reception.")
	defer log.Println("Audio reception finished.")

	portaudio.Initialize()
	defer portaudio.Terminate()

	out := make([]int16, 960) // 20ms of audio at 48kHz
	speakerStream, err := portaudio.OpenDefaultStream(0, 1, 48000, len(out), out) // 0 input channels, 1 output channel
	if err != nil {
		log.Println("Error opening PortAudio output stream:", err)
		return
	}
	defer speakerStream.Close()

	opusDecoder, err := opus.NewDecoder(48000, 1) // 1 channel for Discord voice
	if err != nil {
		log.Println("Error creating Opus decoder:", err)
		return
	}

	err = speakerStream.Start()
	if err != nil {
		log.Println("Error starting PortAudio output stream:", err)
		return
	}

	for { // This is the main loop for receiving audio
		log.Println("receiveAudio: Loop iteration.")
		select {
		case <-stopChan:
			log.Println("Stopping audio receive goroutine (stop signal received).")
			speakerStream.Stop()
			return // Exits here if stop signal received
		default:
			if vc == nil {
				log.Println("VC is nil, returning from receive goroutine.")
				return
			}
			if !vc.Ready {
				log.Println("VC is not ready; waiting for it to become ready.")
				time.Sleep(500 * time.Millisecond)
				continue
			}
			if vc.OpusRecv == nil {
				log.Println("OpusRecv is nil; waiting for voice receive to start.")
				time.Sleep(500 * time.Millisecond)
				continue
			}
			log.Printf("receiveAudio: Before reading from vc.OpusRecv, vc.Ready = %t\n", vc.Ready)
			p, ok := <-vc.OpusRecv // This is a blocking call
			if !ok {
				log.Println("OpusRecv channel closed, returning from receive goroutine.")
				return // Exits here if OpusRecv channel closed
			}
			log.Println("Received audio packet from Discord.")

			_, err := opusDecoder.Decode(p.Opus, out)
			if err != nil {
				log.Println("Error decoding Opus data:", err)
				continue
			}

			if speakerStream != nil {
				err = speakerStream.Write()
				if err != nil {
					log.Println("Error writing to PortAudio output stream:", err)
				}
			}
		}
	}
}
