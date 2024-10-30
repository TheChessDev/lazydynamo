package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
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
	client      *dynamodb.Client
	ddBuffer    string
	filtered    []string
	focus       focus
	lastKeyTime time.Time
	loading     bool
	region      string
	tableInput  textinput.Model
	tables      []string
}

func initialModel() model {
	ti := textinput.New()
	ti.Placeholder = "Search tables..."
	ti.Focus()
	ti.CharLimit = 156
	ti.Width = 20

	return model{
		focus:      focusTableInput,
		region:     "us-east-1",
		tableInput: ti,
		loading:    true,
	}
}

func (m model) Init() tea.Cmd {
	// Load AWS configuration
	cfg, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion(m.region))
	if err != nil {
		log.Fatalf("unable to load SDK config, %v", err)
	}

	// Create DynamoDB client
	m.client = dynamodb.NewFromConfig(cfg)

	// Fetch tables from DynamoDB
	return m.fetchTables
}

// Command to fetch tables from DynamoDB
func (m model) fetchTables() tea.Msg {
	var tableNames []string
	input := &dynamodb.ListTablesInput{}
	paginator := dynamodb.NewListTablesPaginator(m.client, input)

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(context.TODO())
		if err != nil {
			log.Fatalf("failed to fetch tables, %v", err)
		}
		tableNames = append(tableNames, page.TableNames...)
	}

	return tablesFetchedMsg(tableNames)
}

// Message type for fetched tables
type tablesFetchedMsg []string

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tablesFetchedMsg:
		// Update model with the fetched tables and disable loading
		m.tables = msg
		m.filtered = msg
		m.loading = false
		return m, nil

	case tea.KeyMsg:
		// Handle `Escape` key globally to exit edit mode when in the search box
		if msg.String() == "esc" && m.tableInput.Focused() {
			m.tableInput.Blur()
			m.ddBuffer = "" // Clear dd buffer on exiting edit mode
			return m, nil
		}

		// Handle `Enter` key to switch to tables box when in edit mode
		if msg.String() == "enter" && m.tableInput.Focused() {
			m.tableInput.Blur()
			m.focus = focusTableList
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
		Padding(0, 1)
	regionContent := regionBoxStyle.Render(fmt.Sprintf("AWS Region: %s", m.region))

	// Table list box or loading message
	tableListStyle := lipgloss.NewStyle().
		Width(leftWidth).
		Height(containerHeight-11).
		Padding(1, 1).
		Border(lipgloss.RoundedBorder())
	if m.focus == focusTableList {
		tableListStyle = tableListStyle.BorderForeground(lipgloss.Color("10"))
	}
	var tableListContent string
	if m.loading {
		tableListContent = tableListStyle.Render("Loading tables...")
	} else {
		tableListContent = tableListStyle.Render(strings.Join(m.filtered, "\n"))
	}

	// Text input box at the bottom of the left column, with focus color
	leftBottomBoxStyle := lipgloss.NewStyle().
		Width(leftWidth).
		Height(3).
		Padding(0, 1).
		Border(lipgloss.RoundedBorder())
	if m.focus == focusTableInput {
		leftBottomBoxStyle = leftBottomBoxStyle.BorderForeground(lipgloss.Color("10"))
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
		rightBoxStyle = rightBoxStyle.BorderForeground(lipgloss.Color("10"))
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
