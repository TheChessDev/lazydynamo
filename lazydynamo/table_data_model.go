package lazydynamo

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"runtime"
	"sync"
	"time"

	"github.com/TheChessDev/lazydynamo/internals/tools"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type DataFetchedMsg []list.Item

type tableDataRow string

func (i tableDataRow) FilterValue() string { return string(i) }

type tableDataDelegate struct{}

// keyMap defines a set of keybindings. To work for help it must satisfy
// key.Map. It could also very easily be a map[string]key.Binding.
type TableDataKeyMap struct {
	Up   key.Binding
	Down key.Binding
	Help key.Binding
	Quit key.Binding
}

// ShortHelp returns keybindings to be shown in the mini help view. It's part
// of the key.Map interface.
func (k TableDataKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Help, k.Quit}
}

// FullHelp returns keybindings for the expanded help view. It's part of the
// key.Map interface.
func (k TableDataKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down},   // first column
		{k.Help, k.Quit}, // second column
	}
}

var tableDataKeys = TableDataKeyMap{
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "move up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "move down"),
	),
	Help: key.NewBinding(
		key.WithKeys("?"),
		key.WithHelp("?", "toggle help"),
	),
	Quit: key.NewBinding(
		key.WithKeys("q", "esc", "ctrl+c"),
		key.WithHelp("q", "quit"),
	),
}

type TableDataModel struct {
	keys          TableDataKeyMap
	tableData     []list.Item
	selectedTable string
	client        *dynamodb.Client
	dataList      list.Model
}

func (m TableDataModel) New(client *dynamodb.Client) TableDataModel {
	items := []list.Item{}

	l := list.New(items, itemDelegate{}, 10, 10)

	l.SetShowStatusBar(false)
	l.Styles.PaginationStyle = paginationStyle
	l.SetShowHelp(true)
	l.SetShowFilter(true)
	l.KeyMap.Quit.SetKeys("q", "ctrl-c")
	l.AdditionalFullHelpKeys = func() []key.Binding {
		return []key.Binding{keys.SelectCollection}
	}
	l.SetSpinner(spinner.Dot)
	l.Styles.Spinner = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	l.Title = ""
	l.Styles.Title = lipgloss.NewStyle()

	return TableDataModel{
		keys: tableDataKeys,

		selectedTable: "",

		client: client,

		dataList: l,
	}
}

// Command to fetch all data from a DynamoDB table using multiple cores with validated starting keys
func (m TableDataModel) fetchAllData(tableName string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		// Describe the table to get primary key schema
		tableInfo, err := m.client.DescribeTable(ctx, &dynamodb.DescribeTableInput{
			TableName: &tableName,
		})
		if err != nil {
			log.Printf("Failed to describe table: %v", err)
			return FetchErrorMsg{err}
		}

		// Retrieve the primary key attributes
		partitionKey, sortKey, err := extractPrimaryKeyAttributes(tableInfo.Table.KeySchema)
		if err != nil {
			log.Printf("Failed to retrieve primary key schema: %v", err)
			return FetchErrorMsg{err}
		}

		// Get the number of available CPU cores
		numSegments := runtime.NumCPU()
		log.Printf("Using %d segments for parallel scan", numSegments)

		var allItems []list.Item // Store data as single-line JSON strings
		var mu sync.Mutex
		var wg sync.WaitGroup
		errChan := make(chan error, numSegments)

		// Scan each segment concurrently
		for segment := 0; segment < numSegments; segment++ {
			wg.Add(1)
			go func(segment int) {
				defer wg.Done()
				var startKey map[string]types.AttributeValue

				for {
					// Prepare scan input with the segment details and validated ExclusiveStartKey
					input := &dynamodb.ScanInput{
						TableName:         &tableName,
						Limit:             aws.Int32(100),
						Segment:           aws.Int32(int32(segment)),
						TotalSegments:     aws.Int32(int32(numSegments)),
						ExclusiveStartKey: validateExclusiveStartKey(startKey, partitionKey, sortKey),
					}

					output, err := m.client.Scan(ctx, input)
					if err != nil {
						errChan <- err
						return
					}

					// Transform items into JSON strings
					var jsonItems []list.Item
					for _, item := range output.Items {
						mapItem, err := tools.DynamoItemToMap(item)
						if err != nil {
							log.Printf("Error converting item: %v", err)
							continue
						}
						jsonData, err := json.Marshal(mapItem)
						if err != nil {
							log.Printf("Error marshaling item to JSON: %v", err)
							continue
						}
						jsonItems = append(jsonItems, tableDataRow(string(jsonData)))
					}

					// Append transformed items to the shared allItems slice
					mu.Lock()
					allItems = append(allItems, jsonItems...)
					mu.Unlock()

					// Check if more items are available
					if output.LastEvaluatedKey == nil {
						break
					}

					// Update startKey for the next scan in this segment
					startKey = output.LastEvaluatedKey
				}
			}(segment)
		}

		// Wait for all goroutines to finish
		wg.Wait()
		close(errChan)

		// Check if there were any errors
		if err := <-errChan; err != nil {
			log.Printf("Error in parallel scan: %v", err)
			return FetchErrorMsg{err}
		}

		return DataFetchedMsg(allItems)
	}
}

// extractPrimaryKeyAttributes retrieves primary key attributes and their types from the KeySchema
func extractPrimaryKeyAttributes(keySchema []types.KeySchemaElement) (partitionKey string, sortKey *string, err error) {
	for _, keyElement := range keySchema {
		switch keyElement.KeyType {
		case types.KeyTypeHash:
			partitionKey = *keyElement.AttributeName
		case types.KeyTypeRange:
			sortKey = keyElement.AttributeName
		}
	}
	if partitionKey == "" {
		return "", nil, fmt.Errorf("partition key not found in table schema")
	}
	return partitionKey, sortKey, nil
}

// validateExclusiveStartKey ensures that ExclusiveStartKey contains exactly the partition and sort keys
func validateExclusiveStartKey(startKey map[string]types.AttributeValue, partitionKey string, sortKey *string) map[string]types.AttributeValue {
	if startKey == nil {
		return nil
	}

	validatedKey := make(map[string]types.AttributeValue)

	// Validate and add the partition key
	if partitionValue, ok := startKey[partitionKey]; ok {
		validatedKey[partitionKey] = partitionValue
	}

	// Validate and add the sort key if present
	if sortKey != nil {
		if sortValue, ok := startKey[*sortKey]; ok {
			validatedKey[*sortKey] = sortValue
		}
	}
	return validatedKey
}
