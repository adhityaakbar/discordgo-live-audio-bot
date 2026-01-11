package main

import (
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time" // Import time for potential delays

	"github.com/bwmarrin/discordgo"
	"github.com/gordonklaus/portaudio"
	"github.com/joho/godotenv"
	"gopkg.in/hraban/opus.v2"
)

var (
	currentVC       *discordgo.VoiceConnection
	stopAudioStream chan struct{}
)

func main() {
	// Open a file for logging
	logFile, err := os.OpenFile("bot.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
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

	// Create a new Discord session
	dg, err := discordgo.New("Bot " + token)
	if err != nil {
		log.Println("Error creating Discord session:", err)
		return
	}
	dg.Identify.Intents = discordgo.IntentsAll

	// Add a handler for the ready event
	dg.AddHandler(ready)

	// Add a handler for GuildCreate to auto-join the voice channel
	var joined bool
	dg.AddHandler(func(s *discordgo.Session, event *discordgo.GuildCreate) {
		if joined {
			return
		}

		voiceChannelName := os.Getenv("VOICE_CHANNEL_NAME")
		if voiceChannelName == "" {
			log.Println("VOICE_CHANNEL_NAME not found in .env file. Bot will not join a voice channel.")
			return
		}

		for _, c := range event.Guild.Channels {
			if c.Type == discordgo.ChannelTypeGuildVoice && c.Name == voiceChannelName {
				vc, err := s.ChannelVoiceJoin(event.Guild.ID, c.ID, false, true)
				if err != nil {
					log.Printf("Error joining voice channel: %v\n", err)
					return
				}
				log.Printf("Successfully joined voice channel '%s' (%s).\n", voiceChannelName, c.ID)
				joined = true
				currentVC = vc // Assign to global variable
				stopAudioStream = make(chan struct{}) // Initialize the channel
				go streamAudio(s, vc, stopAudioStream)
				return
			}
		}

		log.Printf("Voice channel '%s' not found in guild '%s'.\n", voiceChannelName, event.Guild.Name)
	})

	// Add a handler for messages
	dg.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		// Ignore bot messages
		if m.Author.ID == s.State.User.ID {
			return
		}

		log.Printf("Received message: %s from %s\n", m.Content, m.Author.Username)

		if m.Content == "test bot" {
			s.ChannelMessageSend(m.ChannelID, "test diterima")
			log.Printf("Replied 'test diterima' to 'test bot' from %s\n", m.Author.Username)
		}
	})

	// Open a websocket connection to Discord
	err = dg.Open()
	if err != nil {
		log.Println("Error opening connection:", err)
		return
	}
	log.Println("Discord connection opened successfully.")

	// Wait here until CTRL-C or other term signal is received.
	log.Println("Bot is now running. Press CTRL-C to exit.")
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-sc

	// Cleanly close down the Discord session.
	log.Println("Closing Discord session.")
	
	// Signal streamAudio goroutine to stop and clean up voice connection
	if stopAudioStream != nil {
		close(stopAudioStream)
		// Give some time for the goroutine to clean up
		time.Sleep(1 * time.Second)
	}

	if currentVC != nil && currentVC.Ready {
		log.Println("Disconnecting from voice channel.")
		currentVC.Disconnect()
	}

	dg.Close()
}

func ready(s *discordgo.Session, event *discordgo.Ready) {
	log.Println("Bot is ready!")
	s.UpdateGameStatus(0, "Streaming Audio")
}

func streamAudio(s *discordgo.Session, vc *discordgo.VoiceConnection, stopChan <-chan struct{}) {
	log.Println("Starting audio stream.")
	vc.Speaking(true)
	defer vc.Speaking(false)
	defer log.Println("Audio stream finished.")

	portaudio.Initialize()
	defer portaudio.Terminate()

	// --- Input Stream (Microphone) ---
	in := make([]int16, 960) // 20ms of audio at 48kHz
	micStream, err := portaudio.OpenDefaultStream(1, 0, 48000, len(in), in)
	if err != nil {
		log.Println("Error opening PortAudio input stream:", err)
		return
	}
	defer micStream.Close()

	// --- Output Stream (Speakers) ---
	out := make([]int16, 960)
	speakerStream, err := portaudio.OpenDefaultStream(0, 1, 48000, len(out), out)
	if err != nil {
		log.Println("Error opening PortAudio output stream:", err)
		return
	}
	defer speakerStream.Close()

	// --- Opus Encoder ---
	opusEncoder, err := opus.NewEncoder(48000, 1, opus.AppAudio)
	if err != nil {
		log.Println("Error creating Opus encoder:", err)
		return
	}

	// --- Opus Decoder ---
	opusDecoder, err := opus.NewDecoder(48000, 1)
	if err != nil {
		log.Println("Error creating Opus decoder:", err)
		return
	}

	// --- Goroutine to receive and play audio ---
	go func() {
		for {
			select {
			case <-stopChan:
				log.Println("Stopping audio receive goroutine.")
				return
			default:
				if vc == nil || !vc.Ready {
					log.Println("VC not ready, returning from receive goroutine")
					return
				}
				p, ok := <-vc.OpusRecv
				if !ok {
					log.Println("OpusRecv channel closed, returning from receive goroutine")
					return
				}
				
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
	}()

	err = micStream.Start()
	if err != nil {
		log.Println("Error starting PortAudio input stream:", err)
		return
	}
	err = speakerStream.Start()
	if err != nil {
		log.Println("Error starting PortAudio output stream:", err)
		return
	}

	// --- Main loop to read from mic, encode, and send ---
	for {
		select {
		case <-stopChan:
			log.Println("Stopping audio send goroutine.")
			micStream.Stop()
			speakerStream.Stop()
			return
		default:
			err = micStream.Read()
			if err != nil {
				// log.Println("Error reading from PortAudio input stream:", err)
			}

			opusData := make([]byte, 1000)
			n, err := opusEncoder.Encode(in, opusData)
			if err != nil {
				log.Println("Error encoding Opus data:", err)
				continue
			}

			if vc.Ready {
				vc.OpusSend <- opusData[:n]
			}
		}
	}
}