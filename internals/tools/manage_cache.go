package tools

import (
	"encoding/json"
	"os"
	"time"

	"github.com/charmbracelet/bubbles/list"
)

type Cache struct {
	Data    []string  `json:"data"`
	Updated time.Time `json:"updated"`
}

func LoadCache(cacheFilePath string) (*Cache, error) {
	file, err := os.Open(cacheFilePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var cache Cache
	err = json.NewDecoder(file).Decode(&cache)
	if err != nil {
		return nil, err
	}

	return &cache, nil
}

// Save cache to file
func SaveCache(data []list.Item, cacheDir string, cacheFilePath string) error {
	// Create cache directory if it doesn’t exist
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return err
	}

	var items []string
	for _, value := range data {
		items = append(items, value.FilterValue())
	}

	cache := Cache{
		Data:    items,
		Updated: time.Now(),
	}

	file, err := os.Create(cacheFilePath)
	if err != nil {
		return err
	}
	defer file.Close()

	return json.NewEncoder(file).Encode(cache)
}
