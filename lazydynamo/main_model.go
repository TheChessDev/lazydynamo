package lazydynamo

import (
	"context"
	"io"
	"os"
	"strings"
	"time"

	"fmt"
	"log"

	"github.com/TheChessDev/lazydynamo/internals/components"
	"github.com/TheChessDev/lazydynamo/internals/tools"
	"golang.org/x/term"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type sessionState int

type TablesFetchedMsg []list.Item

const (
	ViewingCollections sessionState = iota
	ViewingData
	ViewMode
)

// keyMap defines a set of keybindings. To work for help it must satisfy
// key.Map. It could also very easily be a map[string]key.Binding.
type keyMap struct {
	Collections      key.Binding
	Data             key.Binding
	Down             key.Binding
	Help             key.Binding
	Left             key.Binding
	Quit             key.Binding
	Right            key.Binding
	Up               key.Binding
	ViewMode         key.Binding
	SelectCollection key.Binding
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
		{k.Collections, k.Data}, // first column
		{k.Help, k.Quit},        // second column
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
	SelectCollection: key.NewBinding(
		key.WithKeys(tea.KeySpace.String()),
		key.WithHelp("space", "Select Collection"),
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
		key.WithKeys(tea.KeyEsc.String()),
		key.WithHelp("esc", "view mode"),
	),
	Quit: key.NewBinding(
		key.WithKeys("q", "ctrl+c"),
		key.WithHelp("q", "quit"),
	),
}

type MainModel struct {
	state          sessionState
	tableDataModel TableDataModel

	keys keyMap
	help help.Model

	client           *dynamodb.Client
	dataScrollOffset int
	ddBuffer         string
	focus            sessionState
	loading          bool
	region           string
	tables           []tableNameItem
	collectionsList  list.Model
}

var (
	titleStyle        = lipgloss.NewStyle().MarginLeft(2)
	itemStyle         = lipgloss.NewStyle().PaddingLeft(4)
	selectedItemStyle = lipgloss.NewStyle().PaddingLeft(2).Foreground(lipgloss.Color("10"))
	paginationStyle   = list.DefaultStyles().PaginationStyle.PaddingLeft(4)
)

type tableNameItem string

func (i tableNameItem) FilterValue() string { return string(i) }

type itemDelegate struct{}

func (d itemDelegate) Height() int                             { return 1 }
func (d itemDelegate) Spacing() int                            { return 0 }
func (d itemDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }
func (d itemDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	i, ok := listItem.(tableNameItem)
	if !ok {
		return
	}

	str := fmt.Sprintf("%s", i)

	fn := itemStyle.Render
	if index == m.Index() {
		fn = func(s ...string) string {
			return selectedItemStyle.Render("> " + strings.Join(s, " "))
		}
	}

	fmt.Fprint(w, fn(str))
}

func New() MainModel {
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

	return MainModel{
		state:           ViewingCollections,
		region:          "us-east-1",
		client:          client,
		loading:         true,
		help:            help.New(),
		keys:            keys,
		tableDataModel:  TableDataModel{}.New(client),
		collectionsList: l,
	}
}

func (m MainModel) Init() tea.Cmd {
	return m.startCollectionsFetch()
}

func (m MainModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		cmds []tea.Cmd
		cmd  tea.Cmd
	)

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		// If we set a width on the help menu it can gracefully truncate
		// its view as needed.
		m.help.Width = msg.Width

		// Base height percentage for the list when width is ample
		baseHeightRatio := 0.7

		// Calculate the aspect ratio (width-to-height ratio)
		aspectRatio := float64(msg.Width) / float64(msg.Height)

		// Adjust the height ratio based on the aspect ratio
		adjustedHeightRatio := baseHeightRatio * aspectRatio
		dataListHeightRation := baseHeightRatio * aspectRatio

		// Clamp the adjustedHeightRatio to a reasonable range, so it doesn't go too low or high
		if adjustedHeightRatio > 2.2 {
			adjustedHeightRatio = 0.7 // Set a maximum height ratio
			dataListHeightRation = 0.8
		} else if adjustedHeightRatio < 2.2 && adjustedHeightRatio > 0.96 {
			adjustedHeightRatio = 0.3
			dataListHeightRation = 0.4
		} else if adjustedHeightRatio < 0.96 {
			adjustedHeightRatio = 0.2
			dataListHeightRation = 0.3
		}

		// Calculate the final list height based on the adjusted height ratio
		collectionListHeight := int(adjustedHeightRatio * float64(msg.Height))
		dataListHeight := int(dataListHeightRation * float64(msg.Height))

		m.collectionsList.SetHeight(collectionListHeight)
		m.tableDataModel.dataList.SetHeight(dataListHeight)
	case TablesFetchedMsg:
		cmd := m.collectionsList.SetItems(msg)
		cmds = append(cmds, cmd, m.collectionsList.ToggleSpinner())
	case TablesFetchStartedMsg:
		cmds = append(cmds, m.fetchCollections(), m.collectionsList.StartSpinner())
	case DataFetchedMsg:
		m.tableDataModel.dataList.SetItems(msg)
		cmds = append(cmds, cmd, m.tableDataModel.dataList.ToggleSpinner())
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

	if m.state == ViewMode {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch {
			case key.Matches(msg, m.keys.Data):
				m.state = ViewingData
				return m, nil
			case key.Matches(msg, m.keys.Collections):
				m.state = ViewingCollections
				m.collectionsList.SetShowHelp(true)
				return m, nil
			}
		}

	}

	if m.state == ViewingCollections {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch {
			case key.Matches(msg, m.keys.ViewMode):
				m.state = ViewMode
				m.collectionsList.SetShowHelp(false)
				return m, nil
			case key.Matches(msg, m.keys.SelectCollection):
				m.tableDataModel.selectedTable = "select collection"
				if !(m.collectionsList.FilterState() == list.Filtering) {
					i, ok := m.collectionsList.SelectedItem().(tableNameItem)
					if ok {
						m.tableDataModel.selectedTable = string(i)
						m.state = ViewingData
					}
					cmds = append(cmds, m.tableDataModel.dataList.StartSpinner(), m.tableDataModel.fetchAllData(m.tableDataModel.selectedTable))
				}
			}
		}

		m.collectionsList, cmd = m.collectionsList.Update(msg)
		cmds = append(cmds, cmd)
	}

	if m.state == ViewingData {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch {
			case key.Matches(msg, m.keys.ViewMode):
				m.state = ViewMode
				return m, nil
			}
		}

		m.tableDataModel.dataList, cmd = m.tableDataModel.dataList.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m MainModel) View() string {
	width, height, err := term.GetSize(int(os.Stdout.Fd()))

	if err != nil {
		fmt.Println("Error getting terminal size:", err)
		return ""
	}

	leftWidth := int(0.3 * float64(width))

	m.collectionsList.SetWidth(leftWidth - 5)

	m.tableDataModel.dataList.SetWidth(width - leftWidth - 10)

	var s string

	boxStyle := components.NewDefaultBoxWithLabel(BoxDefaultColor, lipgloss.Left, lipgloss.Left)

	awsRegionPane := components.NewDefaultBoxWithLabel(BoxDefaultColor, lipgloss.Center, lipgloss.Center)
	tableListPane := boxStyle
	tableDataPane := boxStyle

	helpView := m.help.View(m.keys)

	switch m.state {
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
			tableListPane.Render("Collections", m.collectionsList.View(), leftWidth, height-11),
		),
		tableDataPane.Render("Data", m.tableDataModel.dataList.View(), width-leftWidth-4, height-6),
	)

	if m.state != ViewingCollections {
		s += "\n" + helpView
	}

	return s
}

func (m *MainModel) EditMode() bool {
	return m.state == ViewingCollections || m.state == ViewingData
}

type TablesFetchStartedMsg string

func (m MainModel) startCollectionsFetch() tea.Cmd {
	return func() tea.Msg {
		return TablesFetchStartedMsg("started")
	}
}

// fetchCollections with cache fallback and fetch if cache is missing
func (m MainModel) fetchCollections() tea.Cmd {
	return func() tea.Msg {
		// Attempt to load cached data
		cache, err := tools.LoadCache(CollectionsCacheFilePath)
		if err == nil && time.Since(cache.Updated) < CacheDuration {
			// Return cached data immediately
			go m.refreshCacheInBackground() // Trigger background fetch in the background

			// Convert cached data to list.Item
			var items []list.Item
			for _, value := range cache.Data {
				items = append(items, tableNameItem(value))
			}
			return TablesFetchedMsg(items)
		}

		// If cache is missing or outdated, fetch data and cache it
		return m.fetchAndCacheCollections()
	}
}

// fetchAndCacheCollections performs an immediate fetch from DynamoDB and caches the result
func (m MainModel) fetchAndCacheCollections() tea.Msg {
	var tableNames []list.Item
	input := &dynamodb.ListTablesInput{}
	paginator := dynamodb.NewListTablesPaginator(m.client, input)

	// Fetch table names from DynamoDB
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(context.TODO())
		if err != nil {
			return FetchErrorMsg{err}
		}
		for _, tableName := range page.TableNames {
			tableNames = append(tableNames, tableNameItem(tableName))
		}
	}

	// Cache the fetched data
	if err := tools.SaveCache(tableNames, CacheDir, CollectionsCacheFilePath); err != nil {
		log.Println("Failed to save cache:", err)
	}

	return TablesFetchedMsg(tableNames)
}

// refreshCacheInBackground fetches fresh data and updates the cache in the background
func (m MainModel) refreshCacheInBackground() {
	// Perform a fetch and cache update in the background
	msg := m.fetchAndCacheCollections()
	if fetchMsg, ok := msg.(TablesFetchedMsg); ok {
		// Handle the result if needed (e.g., update the UI with fresh data)
		// This step is optional depending on your app's needs
		log.Println("Cache refreshed in background:", fetchMsg)
	}
}
