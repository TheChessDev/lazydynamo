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
	client        *dynamodb.Client
	ddBuffer      string
	filtered      []string
	focus         focus
	lastKeyTime   time.Time
	loading       bool
	region        string
	scrollOffset  int
	selectedIndex int
	tableInput    textinput.Model
	tables        []string
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
		m.tables = msg
		m.filtered = msg
		m.loading = false
		return m, nil

	case tea.KeyMsg:
		if msg.String() == "esc" && m.tableInput.Focused() {
			m.tableInput.Blur()
			m.ddBuffer = ""
			return m, nil
		}

		if msg.String() == "enter" && m.tableInput.Focused() && !m.loading {
			m.tableInput.Blur()
			m.focus = focusTableList
			return m, nil
		}

		if m.tableInput.Focused() {
			var cmd tea.Cmd
			m.tableInput, cmd = m.tableInput.Update(msg)
			filterText := m.tableInput.Value()
			m.filtered = filterTables(m.tables, filterText)
			m.selectedIndex = 0 // Reset selection when filtering
			m.scrollOffset = 0  // Reset scroll
			return m, cmd
		}

		// Scrollable tables box navigation when focused
		if m.focus == focusTableList {
			switch msg.String() {
			case "j":
				if m.selectedIndex < len(m.filtered)-1 {
					m.selectedIndex++
					if m.selectedIndex >= m.scrollOffset+5 { // Adjust `5` to match visible rows
						m.scrollOffset++
					}
				}
			case "k":
				if m.selectedIndex > 0 {
					m.selectedIndex--
					if m.selectedIndex < m.scrollOffset {
						m.scrollOffset--
					}
				}
			case "l":
				// Action when an item is selected (e.g., go to a details view)
				m.focus = focusRight
				return m, nil
			}
		}

		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit

		case "s":
			m.focus = focusTableInput
			m.tableInput.Focus()
			return m, nil

		case "d":
			if m.focus == focusTableInput && !m.tableInput.Focused() {
				now := time.Now()
				if m.ddBuffer == "d" && now.Sub(m.lastKeyTime) < 500*time.Millisecond {
					m.tableInput.SetValue("")
					m.filtered = filterTables(m.tables, "")
					m.ddBuffer = ""
				} else {
					m.ddBuffer = "d"
					m.lastKeyTime = now
				}
			}

		case "i":
			if m.focus == focusTableInput && !m.tableInput.Focused() {
				m.tableInput.Focus()
				return m, nil
			}

		default:
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
	leftWidth := int(0.3*float64(containerWidth)) - 2

	regionBoxStyle := lipgloss.NewStyle().
		Width(leftWidth).
		Height(3).
		Border(lipgloss.RoundedBorder()).
		Padding(0, 1)
	regionContent := regionBoxStyle.Render(fmt.Sprintf("AWS Region: %s", m.region))

	// Table list box with scrolling and highlighting for selected item
	tableListHeight := containerHeight - 11 // Adjusted for padding and other components
	visibleCount := tableListHeight / 1     // Assuming each row takes 1 line; adjust if needed

	tableListStyle := lipgloss.NewStyle().
		Width(leftWidth).
		Height(tableListHeight).
		Padding(1, 1).
		Border(lipgloss.RoundedBorder())
	if m.focus == focusTableList {
		tableListStyle = tableListStyle.BorderForeground(lipgloss.Color("10"))
	}

	tableListContent := ""
	if m.loading {
		tableListContent = tableListStyle.Render("Loading tables...")
	} else {
		// Determine the maximum items we can display safely
		visibleItems := m.filtered[m.scrollOffset:]
		if len(visibleItems) > visibleCount {
			visibleItems = visibleItems[:visibleCount]
		}

		// Render the items
		for i, table := range visibleItems {
			if i+m.scrollOffset == m.selectedIndex {
				tableListContent += lipgloss.NewStyle().
					Foreground(lipgloss.Color("10")). // Highlight selected item
					Render("> "+table) + "\n"
			} else {
				tableListContent += "  " + table + "\n"
			}
		}
		tableListContent = tableListStyle.Render(tableListContent)
	}

	leftBottomBoxStyle := lipgloss.NewStyle().
		Width(leftWidth).
		Height(3).
		Padding(0, 1).
		Border(lipgloss.RoundedBorder())
	if m.focus == focusTableInput {
		leftBottomBoxStyle = leftBottomBoxStyle.BorderForeground(lipgloss.Color("10"))
	}
	inputContent := leftBottomBoxStyle.Render(m.tableInput.View())

	leftColumn := lipgloss.JoinVertical(lipgloss.Top, regionContent, tableListContent, inputContent)

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

	containerStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Width(containerWidth).
		Height(containerHeight)

	mainContent := lipgloss.JoinHorizontal(lipgloss.Top, leftColumn, rightBoxContent)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, containerStyle.Render(mainContent))
}

func filterTables(tables []string, filterText string) []string {
	filterText = strings.ToLower(filterText)
	var filtered []string

	for _, table := range tables {
		tableLower := strings.ToLower(table)
		matchIndex := 0

		// Fuzzy match: check if all characters of filterText appear in tableLower in order
		for i := 0; i < len(tableLower) && matchIndex < len(filterText); i++ {
			if tableLower[i] == filterText[matchIndex] {
				matchIndex++
			}
		}

		// If we matched all characters in filterText, add table to the results
		if matchIndex == len(filterText) {
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
