package flatpak

import (
	"time"
)

// App represents a Flatpak application from the appstream metadata.
type App struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Summary     string `json:"summary"`
	Description string `json:"description,omitempty"`
	Version     string `json:"version,omitempty"`
	Origin      string `json:"origin"`
}

// Cache holds parsed Flatpak appstream data.
type Cache struct {
	Apps     []App
	LoadedAt time.Time
	Path     string
	Origin   string
}

// SearchResult represents a single search result entry.
type SearchResult struct {
	Name string
	Desc string
}
// This mirrors the searchRow type from the main package.
type searchRow struct {
	Name string
	Desc string
}

// Errors for the flatpak cache package.
var (
	ErrNoCache     = &cacheError{"no valid cache found"}
	ErrCacheStale  = &cacheError{"cache is stale"}
	ErrParseFailed = &cacheError{"failed to parse cache"}
)

type cacheError struct {
	msg string
}

func (e *cacheError) Error() string {
	return e.msg
}
