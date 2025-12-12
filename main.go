package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var (
	geminiFlag   bool
	languageFlag string
	outputFlag   string
)

var rootCmd = &cobra.Command{
	Use:   "parfait <markdown-file>",
	Short: "Generate TTS audio from markdown slides",
	Long: `Parfait generates Text-to-Speech audio files from markdown presentation files.
Each slide's HTML comments (<!-- -->) are converted to speech.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return run(cmd.Context(), args[0])
	},
}

func init() {
	rootCmd.Flags().BoolVarP(&geminiFlag, "gemini", "g", false, "Use Gemini API for TTS (default: use local TTS)")
	rootCmd.Flags().StringVarP(&languageFlag, "lang", "l", "", "Language for TTS (ja/en)")
	rootCmd.Flags().StringVarP(&outputFlag, "output", "o", "", "Output directory for WAV files (default: same directory as input file)")

	rootCmd.MarkFlagRequired("lang")
}

func run(ctx context.Context, mdFile string) error {
	// Validate language flag
	if languageFlag != "ja" && languageFlag != "en" {
		return fmt.Errorf("invalid language: %s. Use ja or en", languageFlag)
	}

	// Validate markdown file exists
	if _, err := os.Stat(mdFile); os.IsNotExist(err) {
		return fmt.Errorf("markdown file '%s' does not exist", mdFile)
	}

	// Validate file extension
	if !strings.HasSuffix(strings.ToLower(mdFile), ".md") {
		return fmt.Errorf("file '%s' is not a markdown file", mdFile)
	}

	// Determine output directory
	outputDir := outputFlag
	if outputDir == "" {
		outputDir = filepath.Dir(mdFile)
	}

	// Check KokoVox service health if using local TTS
	if !geminiFlag {
		if err := checkKokoVoxHealth(); err != nil {
			return err
		}
	}

	fmt.Printf("Processing: %s\n", mdFile)
	fmt.Printf("Output directory: %s\n", outputDir)
	fmt.Printf("Language: %s\n", languageFlag)

	// Run TTS generation
	if err := runTTSGeneration(ctx, mdFile, outputDir, languageFlag, geminiFlag); err != nil {
		return fmt.Errorf("TTS generation failed: %v", err)
	}

	return nil
}

func main() {
	ctx := context.Background()
	if err := rootCmd.ExecuteContext(ctx); err != nil {
		os.Exit(1)
	}
}
