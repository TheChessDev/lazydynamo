package main

import (
	"fmt"
	"golang.org/x/term"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type focus int

const (
	focusRegionBox focus = iota
	focusTableInput
	focusTableList
	focusRight
)

type model struct {
	focus       focus
	region      string
	tableInput  textinput.Model
	tables      []string
	filtered    []string
	ddBuffer    string
	lastKeyTime time.Time
}

func initialModel() model {
	ti := textinput.New()
	ti.Placeholder = "Search tables..."
	ti.Focus()
	ti.CharLimit = 156
	ti.Width = 20

	// Simulate table names for now
	tables := []string{"Users", "Orders", "Products", "Sessions", "Logs", "Customers", "Inventory"}

	return model{
		focus:      focusTableInput,
		region:     "us-east-1",
		tableInput: ti,
		tables:     tables,
		filtered:   tables,
	}
}

func (m model) Init() tea.Cmd {
	return textinput.Blink
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Handle `Escape` key globally to exit edit mode when in the search box
		if msg.String() == "esc" && m.tableInput.Focused() {
			m.tableInput.Blur()
			m.ddBuffer = "" // Clear dd buffer on exiting edit mode
			return m, nil
		}

		// If the text input is focused, process only text input updates and ignore Vim motions
		if m.tableInput.Focused() {
			var cmd tea.Cmd
			m.tableInput, cmd = m.tableInput.Update(msg)

			// Filter table names based on text input
			filterText := m.tableInput.Value()
			m.filtered = filterTables(m.tables, filterText)
			return m, cmd
		}

		// Vim-like key motions (only when not in edit mode)
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit

		case "k":
			if m.focus == focusTableInput {
				m.focus = focusTableList
				m.tableInput.Blur()
			}
		case "j":
			if m.focus == focusTableList {
				m.focus = focusTableInput
				m.tableInput.Focus()
			}
		case "l":
			if m.focus == focusTableInput || m.focus == focusTableList {
				m.focus = focusRight
				m.tableInput.Blur()
			}
		case "h":
			if m.focus == focusRight {
				m.focus = focusTableInput
				m.tableInput.Focus()
			}

		// Pressing `s` to set focus on the search box and activate edit mode
		case "s":
			m.focus = focusTableInput
			m.tableInput.Focus()
			return m, nil

		// Handle `dd` sequence to clear search input
		case "d":
			if m.focus == focusTableInput && !m.tableInput.Focused() {
				now := time.Now()
				if m.ddBuffer == "d" && now.Sub(m.lastKeyTime) < 500*time.Millisecond {
					m.tableInput.SetValue("") // Clear the input
					m.filtered = filterTables(m.tables, "")
					m.ddBuffer = "" // Reset buffer after clearing
				} else {
					m.ddBuffer = "d"
					m.lastKeyTime = now
				}
			}

		// Pressing `i` to focus the input box if it's not already in edit mode
		case "i":
			if m.focus == focusTableInput && !m.tableInput.Focused() {
				m.tableInput.Focus()
				return m, nil // Prevent "i" from being typed into the input
			}

		default:
			// Reset buffer if any key other than "d" is pressed
			m.ddBuffer = ""
		}
	}

	return m, nil
}

func (m model) View() string {
	width, height, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		fmt.Println("Error getting terminal size:", err)
		return ""
	}

	containerWidth := width - 10
	containerHeight := height - 5

	// Define left panel width
	leftWidth := int(0.3*float64(containerWidth)) - 2

	// Region display box at the top of the left column
	regionBoxStyle := lipgloss.NewStyle().
		Width(leftWidth).
		Height(3).
		Border(lipgloss.RoundedBorder()).
		Align(lipgloss.Center).
		Padding(1, 1)
	regionContent := regionBoxStyle.Render(fmt.Sprintf("AWS Region: %s", m.region))

	// Table list box in the middle of the left column, with focus color
	tableListStyle := lipgloss.NewStyle().
		Width(leftWidth).
		Height(containerHeight-11). // Adjust height to fit input box below
		Padding(1, 1).
		Border(lipgloss.RoundedBorder())
	if m.focus == focusTableList {
		tableListStyle = tableListStyle.BorderForeground(lipgloss.Color("10")) // Green color
	}
	tableListContent := tableListStyle.Render(strings.Join(m.filtered, "\n"))

	// Text input box at the bottom of the left column, with focus color
	leftBottomBoxStyle := lipgloss.NewStyle().
		Width(leftWidth).
		Height(3).
		Padding(0, 1).
		Border(lipgloss.RoundedBorder())
	if m.focus == focusTableInput {
		leftBottomBoxStyle = leftBottomBoxStyle.BorderForeground(lipgloss.Color("10")) // Green color
	}
	inputContent := leftBottomBoxStyle.Render(m.tableInput.View())

	// Combine region, table list, and input box vertically in the left column
	leftColumn := lipgloss.JoinVertical(lipgloss.Top, regionContent, tableListContent, inputContent)

	// Define placeholder right box, with focus color
	rightWidth := containerWidth - leftWidth - 4
	rightBoxStyle := lipgloss.NewStyle().
		Width(rightWidth).
		Height(containerHeight-4).
		Border(lipgloss.RoundedBorder()).
		Padding(1, 1)
	if m.focus == focusRight {
		rightBoxStyle = rightBoxStyle.BorderForeground(lipgloss.Color("10")) // Green color
	}
	rightBoxContent := rightBoxStyle.Render("")

	// Main container style
	containerStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Width(containerWidth).
		Height(containerHeight)

	// Arrange layout
	mainContent := lipgloss.JoinHorizontal(lipgloss.Top, leftColumn, rightBoxContent)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, containerStyle.Render(mainContent))
}

func filterTables(tables []string, filterText string) []string {
	var filtered []string
	for _, table := range tables {
		if strings.Contains(strings.ToLower(table), strings.ToLower(filterText)) {
			filtered = append(filtered, table)
		}
	}
	return filtered
}

func main() {
	if _, err := tea.NewProgram(initialModel(), tea.WithAltScreen()).Run(); err != nil {
		fmt.Println("Error running program:", err)
		os.Exit(1)
	}
}
