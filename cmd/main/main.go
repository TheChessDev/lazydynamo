package main

import (
	"fmt"
	"os"

	"github.com/TheChessDev/lazydynamo/lazydynamo"
	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	if _, err := tea.NewProgram(lazydynamo.New(), tea.WithAltScreen(), tea.WithMouseCellMotion()).Run(); err != nil {
		fmt.Println("Error running program:", err)
		os.Exit(1)
	}
}
