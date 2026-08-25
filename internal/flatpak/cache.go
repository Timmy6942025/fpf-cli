package flatpak

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

var DefaultTTL = 24 * time.Hour

var (
	globalCache       *Cache
	globalCacheErr    error
	globalCacheLoaded bool
	globalCacheMu     sync.RWMutex
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
	if len(val) > 32 {
		return DefaultTTL
	}
	if d, err := time.ParseDuration(val); err == nil {
		if d < 0 {
			return DefaultTTL
		}
		if d > 7*24*time.Hour {
			return 7 * 24 * time.Hour
		}
		return d
	}
	if strings.HasSuffix(val, "s") {
		if d, err := time.ParseDuration(val); err == nil {
			if d < 0 {
				return DefaultTTL
			}
			if d > 7*24*time.Hour {
				return 7 * 24 * time.Hour
			}
			return d
		}
	}
	var secs int
	if _, err := fmt.Sscanf(val, "%d", &secs); err == nil {
		if secs < 0 {
			return DefaultTTL
		}
		d := time.Duration(secs) * time.Second
		if d > 7*24*time.Hour {
			return 7 * 24 * time.Hour
		}
		return d
	}
	return DefaultTTL
}

func LoadBest() (*Cache, error) {
	globalCacheMu.RLock()
	if globalCacheLoaded {
		c, err := globalCache, globalCacheErr
		globalCacheMu.RUnlock()
		return c, err
	}
	globalCacheMu.RUnlock()

	globalCacheMu.Lock()
	defer globalCacheMu.Unlock()
	if globalCacheLoaded {
		return globalCache, globalCacheErr
	}
	globalCache, globalCacheErr = loadBestCache()
	globalCacheLoaded = true
	return globalCache, globalCacheErr
}

func ForceReload() {
	globalCacheMu.Lock()
	defer globalCacheMu.Unlock()
	globalCache = nil
	globalCacheErr = nil
	globalCacheLoaded = false
}

func UpdateAppStream() error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "flatpak", "update", "--appstream", "--assumeyes")
	cmd.Env = os.Environ()
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return context.DeadlineExceeded
		}
		return fmt.Errorf("flatpak update --appstream failed: %w", err)
	}
	return nil
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
	if path == "" {
		return nil, fmt.Errorf("empty path")
	}
	if !strings.HasPrefix(path, "/") {
		return nil, fmt.Errorf("path must be absolute: %q", path)
	}
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("path is symlink: %q", path)
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat %q: %w", path, err)
	}
	if info.Size() > 100<<20 {
		return nil, fmt.Errorf("cache file too large: %d", info.Size())
	}

	age := time.Since(info.ModTime())
	if age < 0 {
		age = 0
	}
	if age > CacheTTL() && !ShouldRefreshStaleCache() {
		return nil, ErrCacheStale
	}

	apps, err := ParseAppStreamFile(path)
	if err != nil {
		return nil, fmt.Errorf("parse %q: %w", path, err)
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
	if c == nil || len(c.Apps) == 0 {
		return nil
	}
	if len(query) > 500 {
		query = query[:500]
	}
	if query == "" {
		rows := make([]SearchResult, 0, len(c.Apps))
		for _, app := range c.Apps {
			name := flatpakResultName(app)
			if name == "" || len(name) > 512 {
				continue
			}
			desc := app.Summary
			if desc == "" {
				desc = "-"
			}
			rows = append(rows, SearchResult{
				Name: name,
				Desc: desc,
			})
		}
		return rows
	}

	query = strings.ToLower(query)
	if strings.Contains(query, "\x00") {
		return nil
	}
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
			resultName := flatpakResultName(app)
			if resultName == "" || len(resultName) > 512 {
				continue
			}
			resultDesc := app.Summary
			if resultDesc == "" {
				resultDesc = "-"
			}
			rows = append(rows, SearchResult{
				Name: resultName,
				Desc: resultDesc,
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
