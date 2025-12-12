package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

var (
	geminiFlag   = flag.Bool("gemini", false, "Use Gemini API for TTS (default: use local TTS)")
	languageFlag = flag.String("lang", "", "Language for TTS (ja/en) [required]")
	outputFlag   = flag.String("output", "", "Output directory for WAV files (default: same directory as input file)")
)

func run(ctx context.Context) error {
	args := flag.Args()
	if len(args) == 0 {
		return fmt.Errorf("usage: parfait [options] <markdown-file>\n\nOptions:\n  -gemini   Use Gemini API for TTS (default: use local TTS)\n  -lang     Language for TTS (ja/en) [required]\n  -output   Output directory for WAV files")
	}

	mdFile := args[0]

	// Validate language flag
	if *languageFlag == "" {
		return fmt.Errorf("-lang is required. Specify ja or en")
	}
	if *languageFlag != "ja" && *languageFlag != "en" {
		return fmt.Errorf("invalid language: %s. Use ja or en", *languageFlag)
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
	outputDir := *outputFlag
	if outputDir == "" {
		outputDir = filepath.Dir(mdFile)
	}

	// Check KokoVox service health if using local TTS
	if !*geminiFlag {
		if err := checkKokoVoxHealth(); err != nil {
			return err
		}
	}

	fmt.Printf("Processing: %s\n", mdFile)
	fmt.Printf("Output directory: %s\n", outputDir)
	fmt.Printf("Language: %s\n", *languageFlag)

	// Run TTS generation
	if err := runTTSGeneration(ctx, mdFile, outputDir, *languageFlag, *geminiFlag); err != nil {
		return fmt.Errorf("TTS generation failed: %v", err)
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
