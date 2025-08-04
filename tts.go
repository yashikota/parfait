package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"
	"google.golang.org/genai"
)

// APIKeyManager manages rotation of multiple API keys
type APIKeyManager struct {
	keys  []string
	index int
}

// NewAPIKeyManager creates a new API key manager with rotation
func NewAPIKeyManager() (*APIKeyManager, error) {
	// Load .env file to pick up API keys
	if err := godotenv.Load(); err != nil {
		// .env file is optional, so don't fail if it doesn't exist
		fmt.Printf("Warning: Could not load .env file: %v\n", err)
	}

	var keys []string

	// Check for multiple API keys (GOOGLE_API_KEY_1, GOOGLE_API_KEY_2, etc.)
	for i := 1; i <= 10; i++ {
		keyVar := fmt.Sprintf("GOOGLE_API_KEY_%d", i)
		if key := os.Getenv(keyVar); key != "" {
			keys = append(keys, key)
		}
	}

	// Fallback to single GOOGLE_API_KEY
	if len(keys) == 0 {
		if key := os.Getenv("GOOGLE_API_KEY"); key != "" {
			keys = append(keys, key)
		}
	}

	if len(keys) == 0 {
		return nil, fmt.Errorf("no API keys found. Set GOOGLE_API_KEY or GOOGLE_API_KEY_1, GOOGLE_API_KEY_2, etc")
	}

	fmt.Printf("Loaded %d API key(s) for rotation\n", len(keys))
	return &APIKeyManager{keys: keys, index: 0}, nil
}

// GetNextKey returns the next API key in rotation
func (m *APIKeyManager) GetNextKey() string {
	key := m.keys[m.index]
	m.index = (m.index + 1) % len(m.keys)
	return key
}

// GetAllKeys returns all available API keys for retry logic
func (m *APIKeyManager) GetAllKeys() []string {
	return m.keys
}

// ResetIndex resets the key index to 0
func (m *APIKeyManager) ResetIndex() {
	m.index = 0
}

// writeWAVFile saves raw PCM bytes as a WAV file
func writeWAVFile(filename string, pcmData []byte, channels, sampleRate, bitsPerSample int) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	// Calculate sizes
	dataSize := len(pcmData)
	fileSize := 36 + dataSize

	// Write WAV header
	// "RIFF" chunk descriptor
	file.WriteString("RIFF")
	binary.Write(file, binary.LittleEndian, uint32(fileSize))
	file.WriteString("WAVE")

	// "fmt " sub-chunk
	file.WriteString("fmt ")
	binary.Write(file, binary.LittleEndian, uint32(16))         // Sub-chunk size
	binary.Write(file, binary.LittleEndian, uint16(1))          // Audio format (PCM)
	binary.Write(file, binary.LittleEndian, uint16(channels))   // Number of channels
	binary.Write(file, binary.LittleEndian, uint32(sampleRate)) // Sample rate
	byteRate := sampleRate * channels * bitsPerSample / 8
	binary.Write(file, binary.LittleEndian, uint32(byteRate)) // Byte rate
	blockAlign := channels * bitsPerSample / 8
	binary.Write(file, binary.LittleEndian, uint16(blockAlign))    // Block align
	binary.Write(file, binary.LittleEndian, uint16(bitsPerSample)) // Bits per sample

	// "data" sub-chunk
	file.WriteString("data")
	binary.Write(file, binary.LittleEndian, uint32(dataSize))

	// Write PCM data
	_, err = file.Write(pcmData)
	return err
}

// runTTSGeneration handles TTS generation from notes files
func runTTSGeneration(ctx context.Context) error {
	// Initialize API key manager
	keyManager, err := NewAPIKeyManager()
	if err != nil {
		return err
	}

	// Create audio output directories
	jaAudioDir := filepath.Join("dist", "ja")
	enAudioDir := filepath.Join("dist", "en")
	if err := os.MkdirAll(jaAudioDir, 0755); err != nil {
		return fmt.Errorf("failed to create Japanese audio directory: %v", err)
	}
	if err := os.MkdirAll(enAudioDir, 0755); err != nil {
		return fmt.Errorf("failed to create English audio directory: %v", err)
	}

	// Use channels for parallel processing
	type result struct {
		language string
		err      error
	}
	resultChan := make(chan result, 2)

	// Process Japanese notes in parallel
	go func() {
		jaNotesFile := filepath.Join("dist", "ja", "notes-ja.txt")
		if _, err := os.Stat(jaNotesFile); err == nil {
			fmt.Println("Processing Japanese notes...")
			err := processTTSFile(ctx, keyManager, jaNotesFile, jaAudioDir, "ja")
			if err != nil {
				fmt.Printf("Warning: failed to process Japanese notes: %v\n", err)
			}
			resultChan <- result{"ja", err}
		} else {
			fmt.Printf("Japanese notes file not found: %s\n", jaNotesFile)
			resultChan <- result{"ja", nil}
		}
	}()

	// Process English notes in parallel
	go func() {
		enNotesFile := filepath.Join("dist", "en", "notes-en.txt")
		if _, err := os.Stat(enNotesFile); err == nil {
			fmt.Println("Processing English notes...")
			err := processTTSFile(ctx, keyManager, enNotesFile, enAudioDir, "en")
			if err != nil {
				fmt.Printf("Warning: failed to process English notes: %v\n", err)
			}
			resultChan <- result{"en", err}
		} else {
			fmt.Printf("English notes file not found: %s\n", enNotesFile)
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
			return fmt.Errorf("both Japanese and English TTS failed: ja=%v, en=%v", jaErr, enErr)
		} else if jaErr != nil {
			fmt.Printf("Warning: Japanese TTS failed: %v\n", jaErr)
		} else {
			fmt.Printf("Warning: English TTS failed: %v\n", enErr)
		}
	}

	fmt.Println("TTS generation complete!")
	return nil
}

// processTTSFile processes a single notes file for TTS generation
func processTTSFile(ctx context.Context, keyManager *APIKeyManager, notesFile, outputDir, language string) error {
	// Read notes file
	content, err := os.ReadFile(notesFile)
	if err != nil {
		return fmt.Errorf("failed to read file: %v", err)
	}

	// Split into sections
	sections := splitSections(string(content))
	if len(sections) == 0 {
		return fmt.Errorf("no sections found in the file. Ensure '---' separators are present")
	}

	// Process each section
	for idx, sectionText := range sections {
		fmt.Printf("[TTS] Processing %s section %d/%d (length: %d chars)\n", language, idx+1, len(sections), len(sectionText))

		// Try all API keys for this section
		var lastErr error
		success := false

		for keyAttempt := 0; keyAttempt < len(keyManager.GetAllKeys()); keyAttempt++ {
			// Get next API key
			apiKey := keyManager.GetNextKey()
			keyIndex := (keyManager.index-1+len(keyManager.keys))%len(keyManager.keys) + 1

			fmt.Printf("  Attempting with API key #%d...\n", keyIndex)

			// Create client with current API key
			client, err := genai.NewClient(ctx, &genai.ClientConfig{
				APIKey: apiKey,
			})
			if err != nil {
				fmt.Printf("  Error creating client with API key #%d: %v\n", keyIndex, err)
				lastErr = err
				continue
			}

			config := &genai.GenerateContentConfig{
				ResponseModalities: []string{"AUDIO"},
				SpeechConfig: &genai.SpeechConfig{
					VoiceConfig: &genai.VoiceConfig{
						PrebuiltVoiceConfig: &genai.PrebuiltVoiceConfig{
							VoiceName: "Iapetus",
						},
					},
				},
			}

			// Generate content with TTS
			result, err := client.Models.GenerateContent(ctx, "gemini-2.5-flash-preview-tts", genai.Text(sectionText), config)
			if err != nil {
				// Check if it's a retryable error (429, 500, etc.)
				errStr := err.Error()
				if strings.Contains(errStr, "429") || strings.Contains(errStr, "500") || strings.Contains(errStr, "503") || strings.Contains(errStr, "quota") || strings.Contains(errStr, "rate") {
					fmt.Printf("  Rate limit or server error with API key #%d: %v\n", keyIndex, err)
					lastErr = err
					continue // Try next API key
				} else {
					// Non-retryable error, log and continue to next section
					fmt.Fprintf(os.Stderr, "Error generating %s TTS for section %d (non-retryable): %v\n", language, idx+1, err)
					lastErr = err
					break
				}
			}

			// Extract audio data
			if len(result.Candidates) == 0 || len(result.Candidates[0].Content.Parts) == 0 {
				fmt.Printf("  No audio data found with API key #%d\n", keyIndex)
				lastErr = fmt.Errorf("no audio data found")
				continue
			}

			part := result.Candidates[0].Content.Parts[0]
			if part.InlineData == nil || part.InlineData.Data == nil {
				fmt.Printf("  No inline data found with API key #%d\n", keyIndex)
				lastErr = fmt.Errorf("no inline data found")
				continue
			}

			// Save as WAV file with slide.xxx.wav format
			outputPath := filepath.Join(outputDir, fmt.Sprintf("slide.%03d.wav", idx+1))
			err = writeWAVFile(outputPath, part.InlineData.Data, 1, 24000, 16)
			if err != nil {
				fmt.Printf("  Error saving WAV file with API key #%d: %v\n", keyIndex, err)
				lastErr = err
				continue
			}

			// Success! Show result and break out of retry loop
			relPath, _ := filepath.Rel(".", outputPath)
			fmt.Printf("✓ Saved %s: %s (using API key #%d)\n", language, relPath, keyIndex)
			success = true
			break
		}

		// If all API keys failed for this section
		if !success {
			fmt.Fprintf(os.Stderr, "Failed to generate %s TTS for section %d after trying all API keys. Last error: %v\n", language, idx+1, lastErr)
			// Continue to next section instead of failing completely
		}
	}

	return nil
}

// splitSections splits text by lines that consist solely of '---'
func splitSections(text string) []string {
	var sections []string
	var sectionLines []string

	lines := strings.Split(text, "\n")
	for _, line := range lines {
		if strings.TrimSpace(line) == "---" {
			if len(sectionLines) > 0 {
				section := strings.TrimSpace(strings.Join(sectionLines, "\n"))
				if section != "" {
					sections = append(sections, section)
				}
				sectionLines = nil
			}
		} else {
			sectionLines = append(sectionLines, line)
		}
	}

	// Add last section if any
	if len(sectionLines) > 0 {
		section := strings.TrimSpace(strings.Join(sectionLines, "\n"))
		if section != "" {
			sections = append(sections, section)
		}
	}

	return sections
}
