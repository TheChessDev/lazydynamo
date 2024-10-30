package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type focus int

const (
	focusRegionBox focus = iota
	focusTableInput
	focusTableList
	focusDataBox
)

type model struct {
	client            *dynamodb.Client
	dataScrollOffset  int
	ddBuffer          string
	filtered          []string
	focus             focus
	lastKeyTime       time.Time
	loading           bool
	region            string
	scrollOffset      int
	selectedDataIndex int
	selectedIndex     int
	tableData         []map[string]types.AttributeValue // To store fetched data
	tableInput        textinput.Model
	tables            []string
}

func initialModel() model {
	ti := textinput.New()
	ti.Placeholder = "Search tables..."
	ti.Focus()
	ti.CharLimit = 156
	ti.Width = 20

	// Load AWS config with custom retry settings
	cfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithRegion("us-east-1"),
		config.WithRetryer(func() aws.Retryer {
			return retry.AddWithMaxAttempts(retry.NewStandard(), 5) // 5 retry attempts
		}),
	)
	if err != nil {
		log.Fatalf("unable to load SDK config, %v", err)
	}

	// Create a persistent DynamoDB client
	client := dynamodb.NewFromConfig(cfg)

	return model{
		focus:      focusTableInput,
		region:     "us-east-1",
		tableInput: ti,
		client:     client, // Use persistent client
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

// Error message type for fetching table data
type fetchErrorMsg struct {
	error
}

// Command to fetch data from a selected DynamoDB table
func (m model) fetchTableData(tableName string) tea.Cmd {
	return func() tea.Msg {
		// Use a context with timeout to prevent premature termination
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		output, err := m.client.Scan(ctx, &dynamodb.ScanInput{
			TableName: &tableName,
			Limit:     aws.Int32(100), // Limit to 100 items to prevent excessive loading
		})
		if err != nil {
			return fetchErrorMsg{err}
		}

		// Return the fetched items as a tableDataFetchedMsg
		return tableDataFetchedMsg(output.Items)
	}
}

type tablesFetchedMsg []string
type tableDataFetchedMsg []map[string]types.AttributeValue

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case fetchErrorMsg:
		// Handle the error message
		fmt.Printf("Error fetching data: %v\n", msg.error)
		return m, nil

	case tablesFetchedMsg:
		m.tables = msg
		m.filtered = msg
		m.loading = false
		return m, nil

	case tableDataFetchedMsg:
		m.tableData = msg
		m.selectedDataIndex = 0
		m.dataScrollOffset = 0
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
				// Fetch data for selected table
				selectedTable := m.filtered[m.selectedIndex]
				m.focus = focusDataBox
				return m, m.fetchTableData(selectedTable)
			}
		}

		// Scrollable data box navigation when focused
		if m.focus == focusDataBox {
			switch msg.String() {
			case "j":
				if m.selectedDataIndex < len(m.tableData)-1 {
					m.selectedDataIndex++
					if m.selectedDataIndex >= m.dataScrollOffset+5 { // Adjust for visible rows
						m.dataScrollOffset++
					}
				}
			case "k":
				if m.selectedDataIndex > 0 {
					m.selectedDataIndex--
					if m.selectedDataIndex < m.dataScrollOffset {
						m.dataScrollOffset--
					}
				}
			case " ":
				// Handle space for selecting items (or toggling selection, if implemented)
				fmt.Printf("Selected item: %v\n", m.tableData[m.selectedDataIndex])
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

	// Table list box
	tableListHeight := containerHeight - 11
	visibleCount := tableListHeight / 1

	tableListStyle := lipgloss.NewStyle().
		Width(leftWidth).
		Height(tableListHeight).
		Padding(1, 1).
		Border(lipgloss.RoundedBorder())
	if m.focus == focusTableList {
		tableListStyle = tableListStyle.BorderForeground(lipgloss.Color("10"))
	}
	// Render table list content with scrolling
	visibleItems := m.filtered[m.scrollOffset:]
	if len(visibleItems) > visibleCount {
		visibleItems = visibleItems[:visibleCount]
	}
	tableListContent := ""
	for i, table := range visibleItems {
		if i+m.scrollOffset == m.selectedIndex {
			tableListContent += lipgloss.NewStyle().
				Foreground(lipgloss.Color("10")).
				Render("> "+table) + "\n"
		} else {
			tableListContent += "  " + table + "\n"
		}
	}
	tableListContent = tableListStyle.Render(tableListContent)

	// Text input box
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

	// Right box: Data view
	rightWidth := containerWidth - leftWidth - 4
	rightBoxStyle := lipgloss.NewStyle().
		Width(rightWidth).
		Height(containerHeight-4).
		Border(lipgloss.RoundedBorder()).
		Padding(1, 1)
	if m.focus == focusDataBox {
		rightBoxStyle = rightBoxStyle.BorderForeground(lipgloss.Color("10"))
	}
	// Render data content with scrolling
	visibleData := m.tableData[m.dataScrollOffset:]
	if len(visibleData) > visibleCount {
		visibleData = visibleData[:visibleCount]
	}
	dataContent := ""
	for i, item := range visibleData {
		row := fmt.Sprintf("%v", item)
		if i+m.dataScrollOffset == m.selectedDataIndex {
			dataContent += lipgloss.NewStyle().
				Foreground(lipgloss.Color("10")).
				Render("> "+row) + "\n"
		} else {
			dataContent += "  " + row + "\n"
		}
	}
	rightBoxContent := rightBoxStyle.Render(dataContent)

	// Main container
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
