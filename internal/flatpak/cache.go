package flatpak

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

var DefaultTTL = 24 * time.Hour

var (
	globalCache     *Cache
	globalCacheOnce sync.Once
	globalCacheErr  error
)

func ShouldUseDirectCache() bool {
	val := strings.ToLower(strings.TrimSpace(os.Getenv("FPF_FLATPAK_USE_DIRECT_CACHE")))
	switch val {
	case "0", "false", "no", "off":
		return false
	case "1", "true", "yes", "on":
		return true
	}
	return true
}

func CacheTTL() time.Duration {
	val := strings.TrimSpace(os.Getenv("FPF_FLATPAK_CACHE_TTL"))
	if val == "" {
		return DefaultTTL
	}
	if d, err := time.ParseDuration(val); err == nil {
		return d
	}
	if strings.HasSuffix(val, "s") {
		if d, err := time.ParseDuration(val); err == nil {
			return d
		}
	}
	var secs int
	if _, err := fmt.Sscanf(val, "%d", &secs); err == nil {
		return time.Duration(secs) * time.Second
	}
	return DefaultTTL
}

func LoadBest() (*Cache, error) {
	globalCacheOnce.Do(func() {
		globalCache, globalCacheErr = loadBestCache()
	})
	return globalCache, globalCacheErr
}

func ForceReload() {
	globalCacheOnce = sync.Once{}
	globalCache = nil
	globalCacheErr = nil
}

func RefreshCacheIfNeeded() error {
	cache, err := LoadBest()
	if err != nil {
		return err
	}

	age := time.Since(cache.LoadedAt)
	if age > CacheTTL() {
		go func() {
			_ = exec.Command("flatpak", "update", "--appstream", "--assumeyes").Run()
			ForceReload()
		}()
	}

	return nil
}

func UpdateAppStream() error {
	return exec.Command("flatpak", "update", "--appstream", "--assumeyes").Run()
}

func loadBestCache() (*Cache, error) {
	paths := FindCachePaths()

	for _, path := range paths {
		cache, err := loadFromFile(path)
		if err == nil && len(cache.Apps) > 0 {
			return cache, nil
		}
	}

	_ = UpdateAppStream()

	for _, path := range paths {
		cache, err := loadFromFile(path)
		if err == nil && len(cache.Apps) > 0 {
			return cache, nil
		}
	}

	return nil, ErrNoCache
}

func loadFromFile(path string) (*Cache, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	age := time.Since(info.ModTime())
	if age > CacheTTL() && !ShouldRefreshStaleCache() {
		return nil, ErrCacheStale
	}

	apps, err := ParseAppStreamFile(path)
	if err != nil {
		return nil, err
	}

	if len(apps) == 0 {
		return nil, ErrParseFailed
	}

	origin := extractOriginFromPath(path)

	return &Cache{
		Apps:     apps,
		LoadedAt: time.Now(),
		Path:     path,
		Origin:   origin,
	}, nil
}

func ShouldRefreshStaleCache() bool {
	val := strings.ToLower(strings.TrimSpace(os.Getenv("FPF_FLATPAK_REFRESH_STALE")))
	return val != "0" && val != "false" && val != "no" && val != "off"
}

func extractOriginFromPath(path string) string {
	parts := strings.Split(path, "/flatpak/appstream/")
	if len(parts) < 2 {
		return "unknown"
	}
	remaining := parts[1]
	if idx := strings.Index(remaining, "/"); idx > 0 {
		return remaining[:idx]
	}
	return remaining
}

func (c *Cache) Filter(query string) []SearchResult {
	if query == "" {
		rows := make([]SearchResult, 0, len(c.Apps))
		for _, app := range c.Apps {
			rows = append(rows, SearchResult{
				Name: flatpakResultName(app),
				Desc: app.Summary,
			})
		}
		return rows
	}

	query = strings.ToLower(query)
	rows := make([]SearchResult, 0)

	for _, app := range c.Apps {
		name := strings.ToLower(app.Name)
		summary := strings.ToLower(app.Summary)
		desc := strings.ToLower(app.Description)
		id := strings.ToLower(app.ID)

		if strings.Contains(name, query) ||
			strings.Contains(id, query) ||
			strings.Contains(summary, query) ||
			strings.Contains(desc, query) {
			rows = append(rows, SearchResult{
				Name: flatpakResultName(app),
				Desc: app.Summary,
			})
		}
	}

	return rows
}

func flatpakResultName(app App) string {
	id := strings.TrimSpace(app.ID)
	if id != "" {
		return id
	}
	return strings.TrimSpace(app.Name)
}
