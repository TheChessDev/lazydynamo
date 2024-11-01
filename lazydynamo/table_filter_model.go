package lazydynamo

import (
	"github.com/charmbracelet/bubbles/key"
)

// keyMap defines a set of keybindings. To work for help it must satisfy
// key.Map. It could also very easily be a map[string]key.Binding.
type TableFilterKeyMap struct {
	ExitInsertMode key.Binding
	InsertMode     key.Binding
	ClearSearch    key.Binding
	Help           key.Binding
	Quit           key.Binding
}

// ShortHelp returns keybindings to be shown in the mini help view. It's part
// of the key.Map interface.
func (k TableFilterKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Help, k.Quit}
}

// FullHelp returns keybindings for the expanded help view. It's part of the
// key.Map interface.
func (k TableFilterKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.InsertMode, k.ExitInsertMode}, // first column
		{k.ClearSearch},                  // second column
		{k.Help, k.Quit},                 // third column
	}
}

var tableFilterKeys = TableFilterKeyMap{
	InsertMode: key.NewBinding(
		key.WithKeys("i"),
		key.WithHelp("i", "insert mode"),
	),
	ExitInsertMode: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("ESC", "view mode"),
	),
	ClearSearch: key.NewBinding(
		key.WithKeys("d"),
		key.WithHelp("d", "Clear Search (in view mode)"),
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

type TableFilterModel struct {
	keys TableFilterKeyMap
}

func (m TableFilterModel) New() TableFilterModel {
	return TableFilterModel{
		keys: tableFilterKeys,
	}
}
