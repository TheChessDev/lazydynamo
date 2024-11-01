package tools

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"

	"github.com/charmbracelet/glamour"
)

// RenderJSONWithGlamour takes a JSON string, unmarshals it, pretty-prints it, and then applies glamour styling.
func RenderJSONWithGlamour(rawJSON string) (string, error) {
	// Unmarshal the JSON string to ensure it’s a valid JSON object
	var jsonData interface{}
	if err := json.Unmarshal([]byte(rawJSON), &jsonData); err != nil {
		log.Printf("Failed to unmarshal JSON: %v", err)
		return "", fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	// Pretty-print the JSON with indentation
	prettyJSON, err := json.MarshalIndent(jsonData, "", "  ")
	if err != nil {
		log.Printf("Failed to prettify JSON: %v", err)
		return "", fmt.Errorf("failed to prettify JSON: %w", err)
	}

	// Prepare the JSON content in a markdown code block for glamour
	var buffer bytes.Buffer
	buffer.WriteString("```json\n")
	buffer.Write(prettyJSON)
	buffer.WriteString("\n```")

	// Set up a renderer with a dark theme for glamour
	renderer, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle("dark"),
		glamour.WithWordWrap(80), // Adjust wrap width as needed
	)
	if err != nil {
		log.Printf("Failed to create glamour renderer: %v", err)
		return "", fmt.Errorf("failed to create glamour renderer: %w", err)
	}

	// Render the formatted JSON with glamour
	out, err := renderer.Render(buffer.String())
	if err != nil {
		log.Printf("Failed to render JSON with glamour: %v", err)
		return "", fmt.Errorf("failed to render JSON with glamour: %w", err)
	}

	return out, nil
}

