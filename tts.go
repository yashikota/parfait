package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-audio/audio"
	"github.com/go-audio/wav"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
	"go.abhg.dev/goldmark/frontmatter"
	"google.golang.org/genai"
)

// APIKeyManager manages rotation of multiple API keys
type APIKeyManager struct {
	keys  []string
	index int
}

// NewAPIKeyManager creates a new API key manager with rotation
func NewAPIKeyManager() (*APIKeyManager, error) {
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

const defaultKokoVoxURL = "http://localhost:5108"

// getKokoVoxURL returns the KokoVox service URL from environment or default
func getKokoVoxURL() string {
	if url := os.Getenv("KOKOVOX_URL"); url != "" {
		return url
	}
	return defaultKokoVoxURL
}

// writeWAVFile saves raw PCM bytes as a WAV file with 1 second of silence added at the end
func writeWAVFile(filename string, pcmData []byte, channels, sampleRate, bitsPerSample int) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	// Convert PCM bytes to int samples
	bytesPerSample := bitsPerSample / 8
	numSamples := len(pcmData) / bytesPerSample

	// Add 1 second of silence
	silenceSamples := sampleRate * channels
	totalSamples := numSamples + silenceSamples

	// Create audio buffer
	buf := &audio.IntBuffer{
		Data: make([]int, totalSamples),
		Format: &audio.Format{
			SampleRate:  sampleRate,
			NumChannels: channels,
		},
	}

	// Convert PCM data to int samples (16-bit signed little-endian)
	for i := 0; i < numSamples; i++ {
		offset := i * bytesPerSample
		if bitsPerSample == 16 {
			sample := int16(pcmData[offset]) | int16(pcmData[offset+1])<<8
			buf.Data[i] = int(sample)
		} else if bitsPerSample == 8 {
			buf.Data[i] = int(pcmData[offset]) - 128 // 8-bit is unsigned
		}
	}
	// Silence samples are already 0 (zero-initialized)

	// Create WAV encoder
	enc := wav.NewEncoder(file, sampleRate, bitsPerSample, channels, 1) // 1 = PCM format
	defer enc.Close()

	// Write audio data
	if err := enc.Write(buf); err != nil {
		return fmt.Errorf("failed to write audio data: %v", err)
	}

	return nil
}

// checkKokoVoxHealth checks if KokoVox service is available
func checkKokoVoxHealth() error {
	kokovoxURL := getKokoVoxURL()
	healthURL := fmt.Sprintf("%s/health", kokovoxURL)
	client := &http.Client{
		Timeout: 5 * time.Second,
	}
	resp, err := client.Get(healthURL)
	if err != nil {
		return fmt.Errorf("failed to connect to KokoVox service at %s: %v", kokovoxURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("KokoVox service at %s returned status %d", kokovoxURL, resp.StatusCode)
	}

	fmt.Printf("✓ KokoVox service is available at %s\n", kokovoxURL)
	return nil
}

// generateLocalTTS generates TTS using local TTS service (KokoVox)
func generateLocalTTS(ctx context.Context, text, language string) ([]byte, error) {
	baseURL := getKokoVoxURL()

	// Prepare request body
	requestBody := map[string]interface{}{
		"language": language,
		"text":     text,
	}
	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %v", err)
	}

	// Make HTTP request
	apiURL := fmt.Sprintf("%s/v1/audio/speech", baseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{
		Timeout: 30 * time.Second,
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call TTS API: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("TTS API returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	// Read audio data
	audioData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read audio data: %v", err)
	}

	return audioData, nil
}

// SlideNote represents a slide's note content
type SlideNote struct {
	SlideNumber int
	Note        string
}

// slideInfo holds parsed information for a single slide
type slideInfo struct {
	title    string
	comments []string
}

// extractNotesFromMarkdown extracts HTML comments from a Markdown file using goldmark AST
// Each slide is separated by "---" (ThematicBreak) and comments are in <!-- --> format
// Returns an error if any slide is missing a comment
func extractNotesFromMarkdown(content []byte) ([]SlideNote, error) {
	// goldmark/frontmatterエクステンションを使ってMarkdownをパースします。
	// これにより、フロントマターは自動的に処理され、ASTから除外されます。
	md := goldmark.New(
		goldmark.WithExtensions(
			&frontmatter.Extender{},
		),
	)
	source := content // 後でテキストを抽出するために元のコンテンツを保持します
	reader := text.NewReader(source)
	doc := md.Parser().Parse(reader)

	// Split nodes by ThematicBreak (---) into slides
	slides := splitNodesByThematicBreak(doc, source)

	var notes []SlideNote
	for i, slide := range slides {
		if len(slide.comments) == 0 {
			title := slide.title
			if title == "" {
				title = "(no title)"
			}
			return nil, fmt.Errorf("slide %d (%s) has no comment. All slides must have a <!-- --> comment", i+1, title)
		}

		notes = append(notes, SlideNote{
			SlideNumber: i + 1,
			Note:        strings.Join(slide.comments, "\n"),
		})
	}

	return notes, nil
}

// splitNodesByThematicBreak splits AST nodes by ThematicBreak into slides
// and extracts title and comments for each slide
func splitNodesByThematicBreak(doc ast.Node, source []byte) []slideInfo {
	var slides []slideInfo
	current := slideInfo{}
	hasContent := false

	for child := doc.FirstChild(); child != nil; child = child.NextSibling() {
		switch n := child.(type) {
		case *ast.ThematicBreak:
			// Save current slide and start new one
			if hasContent || current.title != "" || len(current.comments) > 0 {
				slides = append(slides, current)
			}
			current = slideInfo{}
			hasContent = false
		case *ast.Heading:
			if n.Level <= 2 && current.title == "" {
				current.title = extractHeadingText(n, source)
			}
			hasContent = true
		case *ast.HTMLBlock:
			// Extract comment content from HTML block
			comment := extractHTMLComment(n, source)
			if comment != "" {
				current.comments = append(current.comments, comment)
			}
			hasContent = true
		default:
			hasContent = true
		}
	}

	// Add last slide
	if hasContent || current.title != "" || len(current.comments) > 0 {
		slides = append(slides, current)
	}

	return slides
}

// extractHeadingText extracts text content from a heading node
func extractHeadingText(heading *ast.Heading, source []byte) string {
	var buf bytes.Buffer
	for child := heading.FirstChild(); child != nil; child = child.NextSibling() {
		if textNode, ok := child.(*ast.Text); ok {
			buf.Write(textNode.Segment.Value(source))
		}
	}
	return buf.String()
}

// extractHTMLComment extracts comment content from an HTML block
// Returns empty string if not a comment
func extractHTMLComment(block *ast.HTMLBlock, source []byte) string {
	// Only process comment blocks (HTMLBlockType2)
	if block.HTMLBlockType != ast.HTMLBlockType2 {
		return ""
	}

	// Get the raw HTML content
	var buf bytes.Buffer
	for i := 0; i < block.Lines().Len(); i++ {
		line := block.Lines().At(i)
		buf.Write(line.Value(source))
	}
	html := buf.String()

	// Extract content between <!-- and -->
	html = strings.TrimSpace(html)
	if !strings.HasPrefix(html, "<!--") {
		return ""
	}

	// Remove opening <!--
	content := html[4:]

	// Remove closing --> if present
	if idx := strings.Index(content, "-->"); idx != -1 {
		content = content[:idx]
	}

	return strings.TrimSpace(content)
}

// runTTSGeneration handles TTS generation from markdown file
func runTTSGeneration(ctx context.Context, mdFile string, outputDir string, language string, useGemini bool) error {
	var keyManager *APIKeyManager
	var err error

	if useGemini {
		// Initialize API key manager only when using Gemini
		keyManager, err = NewAPIKeyManager()
		if err != nil {
			return err
		}
	}

	// Read markdown file
	content, err := os.ReadFile(mdFile)
	if err != nil {
		return fmt.Errorf("failed to read markdown file: %v", err)
	}

	// Create output directory
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %v", err)
	}

	// Extract notes from markdown
	notes, err := extractNotesFromMarkdown(content)
	if err != nil {
		return err
	}
	if len(notes) == 0 {
		return fmt.Errorf("no notes found in markdown file. Ensure comments are in <!-- --> format")
	}

	fmt.Printf("Found %d slides with notes\n", len(notes))

	// Process each note
	for _, note := range notes {
		fmt.Printf("[TTS] Processing slide %03d (length: %d chars)\n", note.SlideNumber, len(note.Note))

		outputPath := filepath.Join(outputDir, fmt.Sprintf("%03d.wav", note.SlideNumber))

		if useGemini {
			if err := generateGeminiTTS(ctx, keyManager, note.Note, outputPath, language, note.SlideNumber); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to generate TTS for slide %03d: %v\n", note.SlideNumber, err)
				continue
			}
		} else {
			if err := generateLocalTTSToFile(ctx, note.Note, outputPath, language, note.SlideNumber); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to generate TTS for slide %03d: %v\n", note.SlideNumber, err)
				continue
			}
		}
	}

	fmt.Println("TTS generation complete!")
	return nil
}

// generateGeminiTTS generates TTS using Gemini API
func generateGeminiTTS(ctx context.Context, keyManager *APIKeyManager, text, outputPath, language string, slideNum int) error {
	var lastErr error

	// Try all API keys for this section
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
		result, err := client.Models.GenerateContent(ctx, "gemini-2.5-flash-preview-tts", genai.Text(text), config)
		if err != nil {
			// Check if it's a retryable error (429, 500, etc.)
			errStr := err.Error()
			if strings.Contains(errStr, "429") || strings.Contains(errStr, "500") || strings.Contains(errStr, "503") || strings.Contains(errStr, "quota") || strings.Contains(errStr, "rate") {
				fmt.Printf("  Rate limit or server error with API key #%d: %v\n", keyIndex, err)
				lastErr = err
				continue // Try next API key
			} else {
				// Non-retryable error
				return fmt.Errorf("error generating TTS: %v", err)
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

		// Save as WAV file
		err = writeWAVFile(outputPath, part.InlineData.Data, 1, 24000, 16)
		if err != nil {
			fmt.Printf("  Error saving WAV file with API key #%d: %v\n", keyIndex, err)
			lastErr = err
			continue
		}

		// Success!
		fmt.Printf("✓ Saved slide %03d: %s (using API key #%d)\n", slideNum, outputPath, keyIndex)
		return nil
	}

	return fmt.Errorf("failed after trying all API keys: %v", lastErr)
}

// generateLocalTTSToFile generates TTS using local service and saves to file
func generateLocalTTSToFile(ctx context.Context, text, outputPath, language string, slideNum int) error {
	audioData, err := generateLocalTTS(ctx, text, language)
	if err != nil {
		return err
	}

	// Local TTS returns WAV file directly, so we can write it as-is
	err = os.WriteFile(outputPath, audioData, 0644)
	if err != nil {
		return fmt.Errorf("error saving WAV file: %v", err)
	}

	// Success!
	fmt.Printf("✓ Saved slide %03d: %s (using local TTS)\n", slideNum, outputPath)
	return nil
}
