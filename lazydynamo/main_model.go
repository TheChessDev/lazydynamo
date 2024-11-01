package lazydynamo

import (
	"context"
	"os"
	// "encoding/json"
	"fmt"
	"log"

	// "os"
	"sync"
	"time"

	"github.com/TheChessDev/lazydynamo/internals/components"
	"golang.org/x/term"

	// "golang.org/x/term"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/charmbracelet/bubbles/textinput"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type sessionState int

type DataFetchedMsg []map[string]types.AttributeValue
type TablesFetchedMsg []string

type FetchErrorMsg struct{ error }

const (
	FilteringCollections sessionState = iota
	ViewingCollections
	ViewingData
	ViewMode
)

// keyMap defines a set of keybindings. To work for help it must satisfy
// key.Map. It could also very easily be a map[string]key.Binding.
type keyMap struct {
	Collections       key.Binding
	CollectionsFilter key.Binding
	Data              key.Binding
	Down              key.Binding
	Help              key.Binding
	Left              key.Binding
	Quit              key.Binding
	Right             key.Binding
	Up                key.Binding
	ViewMode          key.Binding
}

// ShortHelp returns keybindings to be shown in the mini help view. It's part
// of the key.Map interface.
func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Help, k.Quit}
}

// FullHelp returns keybindings for the expanded help view. It's part of the
// key.Map interface.
func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.CollectionsFilter, k.Collections, k.Data}, // first column
		{k.Help, k.Quit}, // second column
	}
}

var keys = keyMap{
	Data: key.NewBinding(
		key.WithKeys("d"),
		key.WithHelp("d", "Go to Collection Data"),
	),
	Collections: key.NewBinding(
		key.WithKeys("c"),
		key.WithHelp("c", "Go to Collections"),
	),
	CollectionsFilter: key.NewBinding(
		key.WithKeys("s"),
		key.WithHelp("s", "Search Collections"),
	),
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "move up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "move down"),
	),
	Left: key.NewBinding(
		key.WithKeys("left", "h"),
		key.WithHelp("←/h", "move left"),
	),
	Right: key.NewBinding(
		key.WithKeys("right", "l"),
		key.WithHelp("→/l", "move right"),
	),
	Help: key.NewBinding(
		key.WithKeys("?"),
		key.WithHelp("?", "toggle help"),
	),
	ViewMode: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "view mode"),
	),
	Quit: key.NewBinding(
		key.WithKeys("q", "ctrl+c"),
		key.WithHelp("q", "quit"),
	),
}

type MainModel struct {
	state            sessionState
	tableFilterModel TableFilterModel
	tableListModel   TableListModel
	tableDataModel   TableDataModel

	keys keyMap
	help help.Model

	client            *dynamodb.Client
	dataScrollOffset  int
	ddBuffer          string
	filtered          []string
	focus             sessionState
	loading           bool
	region            string
	scrollOffset      int
	selectedDataIndex int
	selectedIndex     int
	tableData         []map[string]types.AttributeValue // To store fetched data
	tableInput        textinput.Model
	tables            []string
}

func New() MainModel {
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

	return MainModel{
		focus:            FilteringCollections,
		region:           "us-east-1",
		tableInput:       ti,
		client:           client,
		loading:          true,
		help:             help.New(),
		keys:             keys,
		tableDataModel:   TableDataModel{}.New(),
		tableFilterModel: TableFilterModel{}.New(),
	}
}

func (m MainModel) Init() tea.Cmd {
	// return tea.Batch(textinput.Blink, m.fetchTables())

	return textinput.Blink
}

func (m MainModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		// If we set a width on the help menu it can gracefully truncate
		// its view as needed.
		m.help.Width = msg.Width
	}

	if !m.EditMode() {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch {
			case key.Matches(msg, m.keys.Help):
				m.help.ShowAll = !m.help.ShowAll
			case key.Matches(msg, m.keys.Quit):
				return m, tea.Quit
			}
		}
	}

	if m.state == FilteringCollections && m.tableInput.Focused() {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch {
			case key.Matches(msg, m.tableFilterModel.keys.ExitInsertMode):
				m.tableInput.Blur()
				return m, nil
			}
		}

		var cmd tea.Cmd
		m.tableInput, cmd = m.tableInput.Update(msg)
		return m, cmd
	}

	if m.state == FilteringCollections && !m.tableInput.Focused() {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch {
			case key.Matches(msg, m.tableFilterModel.keys.ExitInsertMode):
				m.state = ViewMode
				return m, nil
			case key.Matches(msg, m.tableFilterModel.keys.InsertMode):
				m.tableInput.Focus()
				return m, nil
			case key.Matches(msg, m.tableFilterModel.keys.InsertMode):
				m.tableInput.Focus()
				return m, nil
			case key.Matches(msg, m.tableFilterModel.keys.ClearSearch):
				m.tableInput.SetValue("")
				return m, nil
			}
		}
	}

	if m.state == ViewMode {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch {
			case key.Matches(msg, m.keys.Data):
				m.state = ViewingData
				return m, nil
			case key.Matches(msg, m.keys.Collections):
				m.state = ViewingCollections
				return m, nil
			case key.Matches(msg, m.keys.CollectionsFilter):
				m.state = FilteringCollections
				m.tableInput.Focus()
				return m, nil
			}
		}

	}

	return m, nil
}

func (m MainModel) View() string {
	width, height, err := term.GetSize(int(os.Stdout.Fd()))

	if err != nil {
		fmt.Println("Error getting terminal size:", err)
		return ""
	}

	leftWidth := int(0.3 * float64(width))

	var s string
	// model := m.currentFocusedModel()

	boxStyle := components.NewDefaultBoxWithLabel(BoxDefaultColor, lipgloss.Left, lipgloss.Left)

	awsRegionPane := components.NewDefaultBoxWithLabel(BoxDefaultColor, lipgloss.Center, lipgloss.Center)
	tableListPane := boxStyle
	tableFilterPane := boxStyle
	tableDataPane := boxStyle

	helpView := m.help.View(m.keys)

	switch m.state {
	case FilteringCollections:
		helpView = m.help.View(m.tableFilterModel.keys)
		tableFilterPane = components.NewDefaultBoxWithLabel(BoxActiveColor, lipgloss.Left, lipgloss.Left)
	case ViewingData:
		helpView = m.help.View(m.tableDataModel.keys)
		tableDataPane = components.NewDefaultBoxWithLabel(BoxActiveColor, lipgloss.Left, lipgloss.Left)
	case ViewingCollections:
		tableListPane = components.NewDefaultBoxWithLabel(BoxActiveColor, lipgloss.Left, lipgloss.Left)
	}

	s += lipgloss.JoinHorizontal(
		lipgloss.Top,
		lipgloss.JoinVertical(
			lipgloss.Top,
			awsRegionPane.Render("AWS Region", m.region, leftWidth, 3),
			tableListPane.Render("Collections", "top left", leftWidth, height-16),
			tableFilterPane.Render("Search Collections", m.tableInput.View(), leftWidth, 3),
		),
		tableDataPane.Render("Data", "right", width-leftWidth-4, height-6),
	)

	s += "\n" + helpView
	return s
}

func (m *MainModel) EditMode() bool {
	return (m.state == FilteringCollections && m.tableInput.Focused())
}

func (m MainModel) currentFocusedModel() string {
	switch m.state {
	case ViewingData:
		return "TableData"
	case ViewingCollections:
		return "TableList"
	}

	return "TableFilter"
}

// Command to fetch tables from DynamoDB
func (m MainModel) fetchTables() tea.Cmd {
	return func() tea.Msg {
		var tableNames []string
		input := &dynamodb.ListTablesInput{}
		paginator := dynamodb.NewListTablesPaginator(m.client, input)

		for paginator.HasMorePages() {
			page, err := paginator.NextPage(context.TODO())
			if err != nil {
				return FetchErrorMsg{err}
			}
			tableNames = append(tableNames, page.TableNames...)
		}
		return TablesFetchedMsg(tableNames)
	}
}

// Command to fetch all data concurrently from a DynamoDB table
func (m MainModel) fetchAllData(tableName string) tea.Cmd {
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
		return DataFetchedMsg(allItems) // Send message with all fetched data
	}
}
