package lazydynamo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/TheChessDev/lazydynamo/internals/tools"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

type DataFetchedMsg []list.Item

type tableDataRow string

func (i tableDataRow) FilterValue() string { return string(i) }

type tableDataDelegate struct{}

func (d tableDataDelegate) Height() int                             { return 1 }
func (d tableDataDelegate) Spacing() int                            { return 0 }
func (d tableDataDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }
func (d tableDataDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	i, ok := listItem.(tableDataRow)
	if !ok {
		return
	}

	str := fmt.Sprintf("%s", i)

	modelWidth := m.Width()
	maxWidth := modelWidth - 3 // Adjust for padding or any prefix/suffix

	// Trim the JSON string if it exceeds the model width
	if len(str) > maxWidth {
		str = str[:maxWidth-3] + "..." // Truncate and add ellipsis
	}

	fn := itemStyle.Render
	if index == m.Index() {
		fn = func(s ...string) string {
			return selectedItemStyle.Render("> " + strings.Join(s, " "))
		}
	}

	fmt.Fprint(w, fn(str))
}

// keyMap defines a set of keybindings. To work for help it must satisfy
// key.Map. It could also very easily be a map[string]key.Binding.
type TableDataKeyMap struct {
	Up        key.Binding
	Down      key.Binding
	Help      key.Binding
	Quit      key.Binding
	SelectRow key.Binding
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
		{k.SelectRow},    // second column
		{k.Help, k.Quit}, // third column
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
	SelectRow: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "select row"),
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
	selectedRow   string
}

func (m TableDataModel) New(client *dynamodb.Client) TableDataModel {
	items := []list.Item{}

	l := list.New(items, tableDataDelegate{}, 10, 10)

	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.Styles.PaginationStyle = paginationStyle
	l.SetShowHelp(true)
	l.SetShowFilter(true)
	l.KeyMap.Quit.SetKeys("q", "ctrl-c")
	l.AdditionalFullHelpKeys = func() []key.Binding {
		return []key.Binding{tableDataKeys.SelectRow}
	}

	return TableDataModel{
		keys: tableDataKeys,

		selectedTable: "",

		client: client,

		dataList: l,
	}
}

// fetchAllData with cache fallback and fetch if cache is missing
func (m TableDataModel) fetchAllData(tableName string) tea.Cmd {
	return func() tea.Msg {
		// Attempt to load cached data
		cache, err := tools.LoadCache(tableDataCacheFilePath(tableName))
		if err == nil && time.Since(cache.Updated) < CacheDuration {
			// Return cached data immediately
			go m.refreshTableDataCacheInBackground(tableName) // Trigger background fetch

			var items []list.Item
			for _, value := range cache.Data {
				items = append(items, tableDataRow(value))
			}
			return DataFetchedMsg(items)
		}

		// If cache is missing or outdated, fetch fresh data synchronously
		return m.fetchAndCacheTableData(tableName)
	}
}

// fetchAndCacheTableData performs an immediate fetch from DynamoDB, caches the result, and returns it
func (m TableDataModel) fetchAndCacheTableData(tableName string) tea.Msg {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
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
	numSegments := runtime.NumCPU() / 2
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

	// Cache the fetched data
	if err := tools.SaveCache(allItems, CacheDir, tableDataCacheFilePath(tableName)); err != nil {
		log.Println("Failed to save cache:", err)
	}

	return DataFetchedMsg(allItems)
}

// refreshTableDataCacheInBackground fetches fresh data and updates the cache in the background
func (m TableDataModel) refreshTableDataCacheInBackground(tableName string) {
	// Perform a fetch and cache update in the background
	msg := m.fetchAndCacheTableData(tableName)
	if fetchMsg, ok := msg.(DataFetchedMsg); ok {
		// Handle the result if needed (e.g., update the UI with fresh data)
		log.Println("Cache refreshed in background for table data:", fetchMsg)
	}
}

// Helper function to generate a unique cache file path for each table
func tableDataCacheFilePath(tableName string) string {
	return fmt.Sprintf("%s/%s_data_cache.json", CacheDir, tableName)
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
