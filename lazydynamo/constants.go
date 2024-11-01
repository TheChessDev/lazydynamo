package lazydynamo

import (
	"os"
	"path/filepath"
	"time"

	"github.com/charmbracelet/lipgloss"
)

const (
	BoxActiveColor  = lipgloss.Color("10")
	BoxDefaultColor = lipgloss.Color("#ffffff")
)

var (
	CacheDir                 = filepath.Join(os.Getenv("HOME"), ".lazydynamo_cache")
	CollectionsCacheFilePath = filepath.Join(CacheDir, "collections_cache.json")
	CacheDuration            = 72 * time.Hour // Cache expiry duration
)

type FetchErrorMsg struct{ error }
