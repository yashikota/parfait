package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// extractSlideNumber extracts slide number from filename like "slide.001.png"
func extractSlideNumber(filename string) (int, error) {
	re := regexp.MustCompile(`slide\.(\d+)\.png$`)
	matches := re.FindStringSubmatch(filename)
	if len(matches) < 2 {
		return 0, fmt.Errorf("could not extract slide number from %s", filename)
	}
	return strconv.Atoi(matches[1])
}

// getAudioDuration gets the duration of an audio file using ffprobe
func getAudioDuration(audioFile string) (float64, error) {
	// Convert to absolute path for Windows compatibility
	absPath, err := filepath.Abs(audioFile)
	if err != nil {
		return 0, fmt.Errorf("failed to get absolute path for %s: %v", audioFile, err)
	}

	cmd := exec.Command("ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		absPath)

	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("failed to get audio duration for %s: %v", absPath, err)
	}

	duration, err := strconv.ParseFloat(strings.TrimSpace(string(output)), 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse duration from output '%s': %v", strings.TrimSpace(string(output)), err)
	}

	return duration, nil
}

// createVideo creates videos by combining slide images with corresponding audio files
func createVideo(slidesDir, audioDir, outputDir, language string) error {
	// Create output directory if not exists
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %v", err)
	}

	// Get all slide images (slide.001.png, slide.002.png, etc.)
	slidePattern := filepath.Join(slidesDir, "slide.*.png")
	slides, err := filepath.Glob(slidePattern)
	if err != nil {
		return fmt.Errorf("failed to find slides: %v", err)
	}

	if len(slides) == 0 {
		return fmt.Errorf("no slide images found in %s", slidesDir)
	}

	// Sort slides by number
	sort.Slice(slides, func(i, j int) bool {
		numI, errI := extractSlideNumber(slides[i])
		numJ, errJ := extractSlideNumber(slides[j])
		if errI != nil || errJ != nil {
			return slides[i] < slides[j]
		}
		return numI < numJ
	})

	// Process each slide
	for _, slide := range slides {
		slideNum, err := extractSlideNumber(slide)
		if err != nil {
			fmt.Printf("Could not extract slide number from %s: %v\n", slide, err)
			continue
		}

		// Find corresponding audio file (slide.001.wav format)
		audioFile := filepath.Join(audioDir, fmt.Sprintf("slide.%03d.wav", slideNum))

		// Check if audio exists
		if _, err := os.Stat(audioFile); os.IsNotExist(err) {
			fmt.Printf("Audio file %s not found, skipping slide %d\n", audioFile, slideNum)
			continue
		}

		outputFile := filepath.Join(outputDir, fmt.Sprintf("slide-%s-%03d.mp4", language, slideNum))

		// Get audio duration
		audioDuration, err := getAudioDuration(audioFile)
		if err != nil {
			fmt.Printf("Error getting audio duration for %s: %v\n", audioFile, err)
			continue
		}

		// Add 1 second blank at the end
		totalDuration := audioDuration + 1.0

		// Convert paths to absolute paths for Windows compatibility
		absSlide, err := filepath.Abs(slide)
		if err != nil {
			fmt.Printf("Error getting absolute path for slide %s: %v\n", slide, err)
			continue
		}
		absAudio, err := filepath.Abs(audioFile)
		if err != nil {
			fmt.Printf("Error getting absolute path for audio %s: %v\n", audioFile, err)
			continue
		}
		absOutput, err := filepath.Abs(outputFile)
		if err != nil {
			fmt.Printf("Error getting absolute path for output %s: %v\n", outputFile, err)
			continue
		}

		// Create video with ffmpeg - simplified command for Windows compatibility
		cmd := exec.Command("ffmpeg",
			"-y",         // Overwrite output file if exists
			"-loop", "1", // Loop the image
			"-i", absSlide, // Input image
			"-i", absAudio, // Input audio
			"-c:v", "libx264", // Video codec
			"-tune", "stillimage", // Optimize for still image
			"-c:a", "aac", // Audio codec
			"-b:a", "192k", // Audio bitrate
			"-pix_fmt", "yuv420p", // Pixel format for compatibility
			"-shortest",                              // Finish encoding when the shortest input stream ends
			"-t", fmt.Sprintf("%.2f", totalDuration), // Total duration including blank second
			"-vf", "scale=trunc(iw/2)*2:trunc(ih/2)*2", // Ensure dimensions are even
			absOutput)

		fmt.Printf("Creating video for slide %d...\n", slideNum)
		fmt.Printf("Command: ffmpeg -y -loop 1 -i \"%s\" -i \"%s\" -c:v libx264 -tune stillimage -c:a aac -b:a 192k -pix_fmt yuv420p -shortest -t %.2f -vf scale=trunc(iw/2)*2:trunc(ih/2)*2 \"%s\"\n",
			absSlide, absAudio, totalDuration, absOutput)

		// Capture both stdout and stderr
		output, err := cmd.CombinedOutput()
		if err != nil {
			fmt.Printf("Error processing slide %d: %v\n", slideNum, err)
			fmt.Printf("FFmpeg output: %s\n", string(output))
			// Try to get more detailed error information
			if exitError, ok := err.(*exec.ExitError); ok {
				fmt.Printf("Exit code: %d\n", exitError.ExitCode())
			}
			continue
		}
		fmt.Printf("Created %s\n", absOutput)
	}

	return nil
}

// createCombinedVideo creates a single video combining all slides with audio
func createCombinedVideo(outputDir, language string) error {
	// Find all slide videos
	videoPattern := filepath.Join(outputDir, fmt.Sprintf("slide-%s-*.mp4", language))
	slideVideos, err := filepath.Glob(videoPattern)
	if err != nil {
		return fmt.Errorf("failed to find slide videos: %v", err)
	}

	if len(slideVideos) == 0 {
		fmt.Printf("No videos found for language %s\n", language)
		return nil
	}

	// Sort videos
	sort.Strings(slideVideos)

	// Create a file listing all videos to concatenate
	listFile := filepath.Join(outputDir, fmt.Sprintf("filelist-%s.txt", language))
	file, err := os.Create(listFile)
	if err != nil {
		return fmt.Errorf("failed to create file list: %v", err)
	}
	defer file.Close()

	// Write file paths to the list file
	for _, video := range slideVideos {
		absPath, err := filepath.Abs(video)
		if err != nil {
			return fmt.Errorf("failed to get absolute path: %v", err)
		}
		// Use forward slashes for FFmpeg compatibility on Windows
		ffmpegPath := strings.ReplaceAll(absPath, "\\", "/")
		fmt.Fprintf(file, "file '%s'\n", ffmpegPath)
	}
	file.Close()

	outputFile := filepath.Join(outputDir, fmt.Sprintf("video-%s.mp4", language))

	// Small delay to ensure file is written
	time.Sleep(time.Second)

	// Convert paths to absolute paths for Windows compatibility
	absListFile, err := filepath.Abs(listFile)
	if err != nil {
		return fmt.Errorf("failed to get absolute path for list file: %v", err)
	}
	absOutputFile, err := filepath.Abs(outputFile)
	if err != nil {
		return fmt.Errorf("failed to get absolute path for output file: %v", err)
	}

	// Standard concatenation without BGM
	cmd := exec.Command("ffmpeg",
		"-y",
		"-f", "concat",
		"-safe", "0",
		"-i", absListFile,
		"-c", "copy",
		absOutputFile)

	fmt.Printf("Creating combined video for %s...\n", language)
	fmt.Printf("Command: ffmpeg -y -f concat -safe 0 -i \"%s\" -c copy \"%s\"\n", absListFile, absOutputFile)

	// Capture both stdout and stderr
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("FFmpeg output: %s\n", string(output))
		// Try to get more detailed error information
		if exitError, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("error creating combined video (exit code %d): %v", exitError.ExitCode(), err)
		}
		return fmt.Errorf("error creating combined video: %v", err)
	}
	fmt.Printf("Created %s\n", absOutputFile)

	return nil
}
