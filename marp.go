package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// runMarpGeneration handles the Marp generation workflow
func runMarpGeneration() error {
	fmt.Println("Starting Marp generation...")

	// Check if slide files exist
	if _, err := os.Stat("slide-ja.md"); os.IsNotExist(err) {
		return fmt.Errorf("slide-ja.md not found")
	}
	if _, err := os.Stat("slide-en.md"); os.IsNotExist(err) {
		return fmt.Errorf("slide-en.md not found")
	}

	// Ensure dist directories exist
	if err := os.MkdirAll("dist/ja", 0755); err != nil {
		return fmt.Errorf("failed to create dist/ja directory: %v", err)
	}
	if err := os.MkdirAll("dist/en", 0755); err != nil {
		return fmt.Errorf("failed to create dist/en directory: %v", err)
	}

	// Use channels for parallel processing
	type result struct {
		language string
		err      error
	}
	resultChan := make(chan result, 2)

	// Process Japanese Marp generation in parallel
	go func() {
		fmt.Println("Processing Japanese Marp files...")
		err := generateMarpFiles("ja")
		resultChan <- result{"ja", err}
	}()

	// Process English Marp generation in parallel
	go func() {
		fmt.Println("Processing English Marp files...")
		err := generateMarpFiles("en")
		resultChan <- result{"en", err}
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
			return fmt.Errorf("both Japanese and English Marp generation failed: ja=%v, en=%v", jaErr, enErr)
		} else if jaErr != nil {
			fmt.Printf("Warning: Japanese Marp generation failed: %v\n", jaErr)
		} else {
			fmt.Printf("Warning: English Marp generation failed: %v\n", enErr)
		}
	}

	fmt.Println("Marp generation complete!")
	return nil
}

// generateMarpFiles generates all Marp outputs for a specific language
func generateMarpFiles(lang string) error {
	slideFile := fmt.Sprintf("slide-%s.md", lang)
	distDir := filepath.Join("dist", lang)

	// Generate PDF
	pdfOutput := filepath.Join(distDir, fmt.Sprintf("slide-%s.pdf", lang))
	if err := runMarpCommand(slideFile, "--pdf", "--allow-local-files", "-o", pdfOutput); err != nil {
		return fmt.Errorf("PDF generation failed: %v", err)
	}

	// Generate PNG images
	imageOutput := filepath.Join(distDir, "slide.png")
	if err := runMarpCommand(slideFile, "--images", "png", "--output", imageOutput, "--allow-local-files"); err != nil {
		return fmt.Errorf("image generation failed: %v", err)
	}

	// Generate notes
	notesOutput := filepath.Join(distDir, fmt.Sprintf("notes-%s.txt", lang))
	if err := runMarpCommand(slideFile, "--notes", "-o", notesOutput); err != nil {
		return fmt.Errorf("notes generation failed: %v", err)
	}

	fmt.Printf("✓ %s Marp files generated successfully\n", lang)
	return nil
}

// runMarpCommand executes a marp command with the given arguments
func runMarpCommand(args ...string) error {
	cmd := exec.Command("marp", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
