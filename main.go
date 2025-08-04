package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
)

var (
	ttsFlag   = flag.Bool("tts", false, "Generate TTS from notes")
	videoFlag = flag.Bool("video", false, "Create videos from slides and audio")
	marpFlag  = flag.Bool("marp", false, "Generate slides, images, and notes using Marp")
)

// runVideoCreation handles the video creation workflow
func runVideoCreation() error {
	// Check if dist directory exists
	distDir := "dist"
	if _, err := os.Stat(distDir); os.IsNotExist(err) {
		return fmt.Errorf("dist directory '%s' does not exist. Run TTS generation first", distDir)
	}

	// Use channels for parallel processing
	type result struct {
		language string
		err      error
	}
	resultChan := make(chan result, 2)

	// Process Japanese videos in parallel
	go func() {
		jaSlideDir := filepath.Join(distDir, "ja")
		jaAudioDir := filepath.Join(distDir, "ja")
		if _, err := os.Stat(jaSlideDir); err == nil {
			fmt.Println("Processing Japanese videos...")
			err := createVideo(jaSlideDir, jaAudioDir, jaSlideDir, "ja")
			if err != nil {
				fmt.Printf("Warning: failed to create Japanese videos: %v\n", err)
				resultChan <- result{"ja", err}
				return
			}
			err = createCombinedVideo(jaSlideDir, "ja")
			if err != nil {
				fmt.Printf("Warning: failed to create combined Japanese video: %v\n", err)
			}
			resultChan <- result{"ja", err}
		} else {
			fmt.Println("Japanese directory not found, skipping...")
			resultChan <- result{"ja", nil}
		}
	}()

	// Process English videos in parallel
	go func() {
		enSlideDir := filepath.Join(distDir, "en")
		enAudioDir := filepath.Join(distDir, "en")
		if _, err := os.Stat(enSlideDir); err == nil {
			fmt.Println("Processing English videos...")
			err := createVideo(enSlideDir, enAudioDir, enSlideDir, "en")
			if err != nil {
				fmt.Printf("Warning: failed to create English videos: %v\n", err)
				resultChan <- result{"en", err}
				return
			}
			err = createCombinedVideo(enSlideDir, "en")
			if err != nil {
				fmt.Printf("Warning: failed to create combined English video: %v\n", err)
			}
			resultChan <- result{"en", err}
		} else {
			fmt.Println("English directory not found, skipping...")
			resultChan <- result{"en", nil}
		}
	}()

	// Wait for both processes to complete
	var jaErr, enErr error
	for i := 0; i < 2; i++ {
		res := <-resultChan
		if res.language == "ja" {
			jaErr = res.err
		} else {
			enErr = res.err
		}
	}

	// Report any errors
	if jaErr != nil || enErr != nil {
		if jaErr != nil && enErr != nil {
			return fmt.Errorf("both Japanese and English video creation failed: ja=%v, en=%v", jaErr, enErr)
		} else if jaErr != nil {
			fmt.Printf("Warning: Japanese video creation failed: %v\n", jaErr)
		} else {
			fmt.Printf("Warning: English video creation failed: %v\n", enErr)
		}
	}

	fmt.Println("Video creation complete!")
	return nil
}

func run(ctx context.Context) error {
	// If no flags are provided, run the default workflow
	if !*ttsFlag && !*videoFlag && !*marpFlag {
		fmt.Println("Running complete workflow: Marp + TTS generation + Video creation")

		// Step 1: Generate Marp files
		fmt.Println("\n=== Step 1: Marp Generation ===")
		if err := runMarpGeneration(); err != nil {
			return fmt.Errorf("marp generation failed: %v", err)
		}

		// Step 2: Generate TTS
		fmt.Println("\n=== Step 2: TTS Generation ===")
		if err := runTTSGeneration(ctx); err != nil {
			return fmt.Errorf("TTS generation failed: %v", err)
		}

		// Step 3: Create videos
		fmt.Println("\n=== Step 3: Video Creation ===")
		if err := runVideoCreation(); err != nil {
			return fmt.Errorf("video creation failed: %v", err)
		}

		fmt.Println("\n=== Complete workflow finished! ===")
		return nil
	}

	// Handle individual flags
	if *marpFlag {
		if err := runMarpGeneration(); err != nil {
			return fmt.Errorf("marp generation failed: %v", err)
		}
	}

	if *videoFlag {
		if err := runVideoCreation(); err != nil {
			return fmt.Errorf("video creation failed: %v", err)
		}
	}

	if *ttsFlag {
		if err := runTTSGeneration(ctx); err != nil {
			return fmt.Errorf("TTS generation failed: %v", err)
		}
	}

	return nil
}

func main() {
	flag.Parse()

	ctx := context.Background()
	if err := run(ctx); err != nil {
		log.Fatal(err)
	}
}
