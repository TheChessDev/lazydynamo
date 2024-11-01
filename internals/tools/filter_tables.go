package tools

import (
	"strings"
)

func FilterTables(tables []string, filterText string) []string {
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
