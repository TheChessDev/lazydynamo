package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
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
	cfg, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion("us-east-1"),
		config.WithRetryer(func() aws.Retryer {
			return retry.AddWithMaxAttempts(retry.NewStandard(), 5)
		}),
	)

	if err != nil {
		log.Fatalf("unable to load SDK config, %v", err)
	}

	client := dynamodb.NewFromConfig(cfg)

	return model{
		focus:      focusTableInput,
		region:     "us-east-1",
		tableInput: ti,
		client:     client,
		loading:    true,
	}
}

// Command to fetch all data concurrently from a DynamoDB table
func (m model) fetchAllData(tableName string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		var allItems []map[string]types.AttributeValue
		var wg sync.WaitGroup
		var mu sync.Mutex
		lastEvaluatedKeys := []map[string]types.AttributeValue{nil} // Start with initial key

		for _, startKey := range lastEvaluatedKeys {
			wg.Add(1)
			go func(startKey map[string]types.AttributeValue) {
				defer wg.Done()
				for {
					input := &dynamodb.ScanInput{
						TableName:         &tableName,
						Limit:             aws.Int32(100),
						ExclusiveStartKey: startKey,
					}
					output, err := m.client.Scan(ctx, input)
					if err != nil {
						log.Printf("Failed to fetch data from DynamoDB: %v", err)
						return
					}

					mu.Lock()
					allItems = append(allItems, output.Items...)
					mu.Unlock()

					if output.LastEvaluatedKey == nil {
						break
					}
					startKey = output.LastEvaluatedKey
				}
			}(startKey)
		}

		wg.Wait()
		return dataFetchedMsg(allItems) // Send message with all fetched data
	}
}

type tablesFetchedMsg []string
type dataFetchedMsg []map[string]types.AttributeValue
type fetchErrorMsg struct{ error }

func (m model) Init() tea.Cmd {
	return m.fetchTables()
}

// Command to fetch tables from DynamoDB
func (m model) fetchTables() tea.Cmd {
	return func() tea.Msg {
		var tableNames []string
		input := &dynamodb.ListTablesInput{}
		paginator := dynamodb.NewListTablesPaginator(m.client, input)

		for paginator.HasMorePages() {
			page, err := paginator.NextPage(context.TODO())
			if err != nil {
				return fetchErrorMsg{err}
			}
			tableNames = append(tableNames, page.TableNames...)
		}
		return tablesFetchedMsg(tableNames)
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case fetchErrorMsg:
		fmt.Printf("Error fetching data: %v\n", msg.error)
		return m, nil

	case tablesFetchedMsg:
		m.tables = msg
		m.filtered = msg
		m.loading = false
		return m, nil

	case dataFetchedMsg:
		m.tableData = msg
		m.selectedDataIndex = 0
		m.dataScrollOffset = 0
		m.loading = false
		m.focus = focusDataBox
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
			m.selectedIndex = 0
			m.scrollOffset = 0
			return m, cmd
		}

		if m.focus == focusTableList {
			switch msg.String() {
			case "j":
				if m.selectedIndex < len(m.filtered)-1 {
					m.selectedIndex++
					if m.selectedIndex >= m.scrollOffset+5 {
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
				m.tableData = nil
				m.dataScrollOffset = 0
				m.selectedDataIndex = 0
				m.focus = focusDataBox
				selectedTable := m.filtered[m.selectedIndex]
				return m, m.fetchAllData(selectedTable)
			}
		}

		if m.focus == focusDataBox {
			switch msg.String() {
			case "j":
				if m.selectedDataIndex < len(m.tableData)-1 {
					m.selectedDataIndex++
					if m.selectedDataIndex >= m.dataScrollOffset+5 {
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
				fmt.Printf("Selected item: %v\n", m.tableData[m.selectedDataIndex])
			}
		}

		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
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
	if m.focus == focusDataBox {
		rightBoxStyle = rightBoxStyle.BorderForeground(lipgloss.Color("10"))
	}

	visibleData := m.tableData[m.dataScrollOffset:]
	if len(visibleData) > visibleCount {
		visibleData = visibleData[:visibleCount]
	}
	dataContent := ""
	for i, item := range visibleData {
		goMap, err := dynamoItemToMap(item)
		if err != nil {
			dataContent += fmt.Sprintf("Error: %v\n", err)
			continue
		}
		jsonData, _ := json.Marshal(goMap)
		row := string(jsonData)

		maxWidth := rightWidth - 4
		if len(row) > maxWidth {
			row = row[:maxWidth-3] + "..."
		}

		if i+m.dataScrollOffset == m.selectedDataIndex {
			dataContent += lipgloss.NewStyle().
				Foreground(lipgloss.Color("10")).
				Render("> "+row) + "\n"
		} else {
			dataContent += "  " + row + "\n"
		}
	}
	rightBoxContent := rightBoxStyle.Render(dataContent)

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

		for i := 0; i < len(tableLower) && matchIndex < len(filterText); i++ {
			if tableLower[i] == filterText[matchIndex] {
				matchIndex++
			}
		}
		if matchIndex == len(filterText) {
			filtered = append(filtered, table)
		}
	}
	return filtered
}

func dynamoItemToMap(item map[string]types.AttributeValue) (map[string]interface{}, error) {
	result := make(map[string]interface{})
	for key, value := range item {
		var err error
		result[key], err = attributeValueToInterface(value)
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func attributeValueToInterface(av types.AttributeValue) (interface{}, error) {
	switch v := av.(type) {
	case *types.AttributeValueMemberS:
		return v.Value, nil
	case *types.AttributeValueMemberN:
		return v.Value, nil
	case *types.AttributeValueMemberBOOL:
		return v.Value, nil
	case *types.AttributeValueMemberSS:
		return v.Value, nil
	case *types.AttributeValueMemberNS:
		return v.Value, nil
	case *types.AttributeValueMemberL:
		list := make([]interface{}, len(v.Value))
		for i, item := range v.Value {
			val, err := attributeValueToInterface(item)
			if err != nil {
				return nil, err
			}
			list[i] = val
		}
		return list, nil
	case *types.AttributeValueMemberM:
		m := make(map[string]interface{})
		for key, item := range v.Value {
			val, err := attributeValueToInterface(item)
			if err != nil {
				return nil, err
			}
			m[key] = val
		}
		return m, nil
	default:
		return nil, fmt.Errorf("unsupported AttributeValue type %T", v)
	}
}

func main() {
	if _, err := tea.NewProgram(initialModel(), tea.WithAltScreen()).Run(); err != nil {
		fmt.Println("Error running program:", err)
		os.Exit(1)
	}
}
