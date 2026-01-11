package main

import (
	"fmt"
	"math"
	"time"

	"github.com/gordonklaus/portaudio"
)

const (
	sampleRate = 44100 // samples per second
	channels   = 1     // mono
	duration   = 5     // seconds
	frequency  = 440   // A4 note, Hz
)

func main() {
	fmt.Println("Starting PortAudio speaker test...")

	// Initialize PortAudio
	err := portaudio.Initialize()
	if err != nil {
		fmt.Printf("Error initializing PortAudio: %v\n", err)
		return
	}
	defer portaudio.Terminate()

	// Create a buffer for audio samples
	bufferSize := 512
	outputBuffer := make([]float32, bufferSize*channels)

	// Open a default output stream
	stream, err := portaudio.OpenDefaultStream(0, channels, sampleRate, len(outputBuffer), outputBuffer)
	if err != nil {
		fmt.Printf("Error opening default stream: %v\n", err)
		return
	}
	defer stream.Close()

	// Start the stream
	err = stream.Start()
	if err != nil {
		fmt.Printf("Error starting stream: %v\n", err)
		return
	}
	defer stream.Stop()

	fmt.Printf("Playing a %d Hz sine wave for %d seconds...\n", frequency, duration)

	// Generate and write sine wave data
	totalSamples := sampleRate * duration
	samplesWritten := 0
	for samplesWritten < totalSamples {
		for i := 0; i < bufferSize; i++ {
			// Generate a sine wave sample
			t := float64(samplesWritten+i) / sampleRate
			sample := float32(math.Sin(2 * math.Pi * frequency * t))
			outputBuffer[i] = sample
		}

		// Write the buffer to the stream.
		err = stream.Write()
		if err != nil {
			fmt.Printf("Error writing to stream: %v\n", err)
			return
		}
		samplesWritten += bufferSize
	}

	fmt.Println("Finished playing speaker test.")
	time.Sleep(1 * time.Second) // Give a moment for audio to finish
}
