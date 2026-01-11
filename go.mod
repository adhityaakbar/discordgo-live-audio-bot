module discord-audio-stream

go 1.22

require (
	github.com/bwmarrin/discordgo v0.29.0
	github.com/gordonklaus/portaudio v0.0.0-20250206071425-98a94950218b
	github.com/joho/godotenv v1.5.1
	gopkg.in/hraban/opus.v2 v2.0.0-20230925203106-0188a62cb302
)

require (
	github.com/darui3018823/discordgo v0.29.0-patched-2 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	golang.org/x/crypto v0.46.0 // indirect
	golang.org/x/sys v0.40.0 // indirect
)

replace github.com/bwmarrin/discordgo => ./discordgo
