package main

import (
	"fmt"
	"os"

	"github.com/TheChessDev/lazydynamo/tui"
	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	var f *os.File

	// Create a temporary file for logging in the OS's temp directory
	tempFile, err := os.CreateTemp("", "lazydynamo-debug-*.log")
	if err != nil {
		fmt.Println("Couldn't create a temporary log file:", err)
		os.Exit(1)
	}
	f = tempFile

	// Set up logging to the temporary file
	tea.LogToFile(f.Name(), "lazydynamo")

	defer func() {
		f.Close()           // Close the file
		os.Remove(f.Name()) // Remove the file when done (if desired)
	}()

	if _, err := tea.NewProgram(lazydynamo.New(), tea.WithAltScreen(), tea.WithMouseCellMotion()).Run(); err != nil {
		fmt.Println("Error running program:", err)
		os.Exit(1)
	}
}
