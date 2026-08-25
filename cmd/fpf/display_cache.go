package main

import (
	"fmt"
	"hash/crc32"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

func isSafeCachePath(path string) bool {
	if !filepath.IsAbs(path) {
		return false
	}
	if strings.Contains(path, "..") {
		return false
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return false
		}
	}
	cleaned := filepath.Clean(path)
	tempDir := filepath.Clean(os.TempDir())
	home, _ := os.UserHomeDir()
	home = filepath.Clean(home)
	xdg := filepath.Clean(strings.TrimSpace(os.Getenv("XDG_CACHE_HOME")))
	isUnder := func(base, p string) bool {
		if base == "" || base == "." {
			return false
		}
		if p == base {
			return true
		}
		if strings.HasPrefix(p, base+string(os.PathSeparator)) {
			return true
		}
		return false
	}
	if isUnder(tempDir, cleaned) {
		return true
	}
	if home != "" && home != "." && isUnder(home, cleaned) {
		return true
	}
	if xdg != "" && xdg != "." && isUnder(xdg, cleaned) {
		return true
	}
	return false
}

func cacheRootPath() string {
	if override := strings.TrimSpace(os.Getenv("FPF_CACHE_DIR")); override != "" {
		if isSafeCachePath(override) {
			return override
		}
		fmt.Fprintf(os.Stderr, "fpf warning: invalid FPF_CACHE_DIR %q, falling back to default\n", override)
	}

	if runtime.GOOS == "windows" {
		if local := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); local != "" {
			if filepath.IsAbs(local) && !strings.Contains(local, "..") {
				return filepath.Join(local, "fpf")
			}
		}
		if app := strings.TrimSpace(os.Getenv("APPDATA")); app != "" {
			if filepath.IsAbs(app) && !strings.Contains(app, "..") {
				return filepath.Join(app, "fpf")
			}
		}
	}

	if xdg := strings.TrimSpace(os.Getenv("XDG_CACHE_HOME")); xdg != "" {
		if filepath.IsAbs(xdg) && !strings.Contains(xdg, "..") {
			return filepath.Join(xdg, "fpf")
		}
		fmt.Fprintf(os.Stderr, "fpf warning: invalid XDG_CACHE_HOME %q, ignoring\n", xdg)
	}
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		if filepath.IsAbs(home) && !strings.Contains(home, "..") {
			return filepath.Join(home, ".cache", "fpf")
		}
	}
	// Fallback to UserHomeDir if HOME env missing
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		home = strings.TrimSpace(home)
		if filepath.IsAbs(home) && !strings.Contains(home, "..") {
			return filepath.Join(home, ".cache", "fpf")
		}
	}
	// Ultimate fallback: TempDir
	tmp := os.TempDir()
	if tmp == "" {
		tmp = "/tmp"
	}
	if info, err := os.Lstat(tmp); err == nil && info.Mode()&os.ModeSymlink != 0 {
		fmt.Fprintf(os.Stderr, "fpf warning: TempDir %q is symlink, using /tmp\n", tmp)
		tmp = "/tmp"
	}
	return filepath.Join(tmp, "fpf-cache")
}

func queryCacheEnabledForManager(manager string) bool {
	if bypass := strings.ToLower(strings.TrimSpace(os.Getenv("FPF_BYPASS_QUERY_CACHE"))); bypass == "1" || bypass == "true" || bypass == "yes" || bypass == "on" {
		return false
	}

	setting := strings.ToLower(strings.TrimSpace(os.Getenv("FPF_ENABLE_QUERY_CACHE")))
	switch setting {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}

	switch manager {
	case "apt", "brew", "pacman", "bun":
		return true
	default:
		return false
	}
}

func queryCacheWriteEnabledForManager(manager string) bool {
	if skipWrite := strings.ToLower(strings.TrimSpace(os.Getenv("FPF_SKIP_QUERY_CACHE_WRITE"))); skipWrite == "1" || skipWrite == "true" || skipWrite == "yes" || skipWrite == "on" {
		return false
	}
	return queryCacheEnabledForManager(manager)
}

func queryCacheTTLSeconds(manager string) int {
	defaults := map[string]int{
		"apt":    180,
		"brew":   120,
		"pacman": 180,
		"bun":    300,
	}

	base := defaults[manager]
	if base <= 0 {
		base = 0
	}
	// Global TTL override with validation
	if raw := strings.TrimSpace(os.Getenv("FPF_QUERY_CACHE_TTL")); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v >= 0 {
			if v > 86400 {
				fmt.Fprintf(os.Stderr, "fpf warning: FPF_QUERY_CACHE_TTL %d exceeds max 86400, capping\n", v)
				v = 86400
			}
			base = v
		} else if raw != "" {
			fmt.Fprintf(os.Stderr, "fpf warning: invalid FPF_QUERY_CACHE_TTL %q, using default %d\n", raw, base)
		}
	}

	managerEnv := map[string]string{
		"apt":    "FPF_APT_QUERY_CACHE_TTL",
		"brew":   "FPF_BREW_QUERY_CACHE_TTL",
		"pacman": "FPF_PACMAN_QUERY_CACHE_TTL",
		"bun":    "FPF_BUN_QUERY_CACHE_TTL",
	}
	if envName, ok := managerEnv[manager]; ok {
		if raw := strings.TrimSpace(os.Getenv(envName)); raw != "" {
			if v, err := strconv.Atoi(raw); err == nil && v >= 0 {
				if v > 86400 {
					fmt.Fprintf(os.Stderr, "fpf warning: %s %d exceeds max 86400, capping\n", envName, v)
					v = 86400
				}
				base = v
			} else {
				fmt.Fprintf(os.Stderr, "fpf warning: invalid %s %q, using %d\n", envName, raw, base)
			}
		}
	}

	if base < 0 {
		base = 0
	}
	if base > 86400 {
		base = 86400
	}
	return base
}

func queryCacheKey(manager, query string, limit, npmLimit int) string {
	// Include all env vars that affect query results for correct fingerprinting.
	// Bump version to v3 when env set expands to invalidate old caches.
	payload := fmt.Sprintf("v3|mgr=%s|q=%s|limit=%d|npm=%d|qlim=%s|nqlim=%s|qper=%s|jsper=%s|npmper=%s|nonpm=%s|bin=%s",
		manager, query, limit, npmLimit,
		os.Getenv("FPF_QUERY_RESULT_LIMIT"),
		os.Getenv("FPF_NO_QUERY_RESULT_LIMIT"),
		os.Getenv("FPF_QUERY_PER_MANAGER_LIMIT"),
		os.Getenv("FPF_JS_QUERY_PER_MANAGER_LIMIT"),
		os.Getenv("FPF_NPM_QUERY_PER_MANAGER_LIMIT"),
		os.Getenv("FPF_NO_QUERY_NPM_LIMIT"),
		binaryIdentityForManager(manager),
	)
	return fmt.Sprintf("%08x", crc32.ChecksumIEEE([]byte(payload)))
}

func queryCachePaths(manager, key string) (string, string) {
	baseDir := filepath.Join(cacheRootPath(), "go-query", manager)
	return filepath.Join(baseDir, key+".tsv"), filepath.Join(baseDir, key+".meta")
}

func loadQueryRowsFromCache(manager, query string, limit, npmLimit int) ([]searchRow, bool) {
	if override := strings.TrimSpace(os.Getenv("FPF_CACHE_DIR")); override != "" && !isSafeCachePath(override) {
		fmt.Fprintf(os.Stderr, "fpf warning: invalid FPF_CACHE_DIR %q, ignoring cache\n", override)
		return nil, false
	}
	if !isManagerSupported(manager) {
		return nil, false
	}
	if !queryCacheEnabledForManager(manager) {
		return nil, false
	}
	if err := validateQuery(query); err != nil {
		return nil, false
	}
	if limit < 0 || limit > 1000 {
		return nil, false
	}
	if npmLimit < 0 || npmLimit > 1000 {
		return nil, false
	}

	ttl := queryCacheTTLSeconds(manager)
	if ttl <= 0 {
		return nil, false
	}

	key := queryCacheKey(manager, query, limit, npmLimit)
	cacheFile, metaFile := queryCachePaths(manager, key)

	// Validate cache paths are safe
	if !isSafeCachePath(cacheFile) || !isSafeCachePath(metaFile) {
		return nil, false
	}
	// Check for symlink
	if info, err := os.Lstat(cacheFile); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, false
	}
	if info, err := os.Lstat(metaFile); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, false
	}

	rawMeta, err := os.ReadFile(metaFile)
	if err != nil {
		return nil, false
	}
	if len(rawMeta) > 8192 {
		return nil, false
	}
	meta := parseMetaMap(rawMeta)
	createdEpoch, err := strconv.ParseInt(meta["created_epoch"], 10, 64)
	if err != nil {
		return nil, false
	}
	if createdEpoch <= 0 || createdEpoch > time.Now().Unix()+60 {
		return nil, false
	}
	if time.Now().Unix()-createdEpoch > int64(ttl) {
		return nil, false
	}
	if meta["fingerprint"] != queryCacheFingerprint(manager, query, limit, npmLimit) {
		return nil, false
	}

	raw, err := os.ReadFile(cacheFile)
	if err != nil {
		return nil, false
	}
	if len(raw) > 10<<20 {
		fmt.Fprintf(os.Stderr, "fpf warning: cache file too large %q (%d bytes), ignoring\n", cacheFile, len(raw))
		return nil, false
	}

	rows := make([]searchRow, 0)
	for _, line := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) == 0 || parts[0] == "" {
			continue
		}
		if len(parts[0]) > maxPackageLength {
			continue
		}
		desc := "-"
		if len(parts) == 2 && strings.TrimSpace(parts[1]) != "" {
			desc = parts[1]
		}
		rows = append(rows, searchRow{Name: parts[0], Desc: desc})
	}

	if len(rows) == 0 {
		return nil, false
	}

	return rows, true
}

func storeQueryRowsToCache(manager, query string, limit, npmLimit int, rows []searchRow) {
	if override := strings.TrimSpace(os.Getenv("FPF_CACHE_DIR")); override != "" && !isSafeCachePath(override) {
		fmt.Fprintf(os.Stderr, "fpf warning: invalid FPF_CACHE_DIR %q, ignoring cache write\n", override)
		return
	}
	if !isManagerSupported(manager) {
		return
	}
	if !queryCacheWriteEnabledForManager(manager) {
		return
	}
	if len(rows) == 0 {
		return
	}
	if err := validateQuery(query); err != nil {
		return
	}
	ttl := queryCacheTTLSeconds(manager)
	if ttl <= 0 {
		return
	}

	key := queryCacheKey(manager, query, limit, npmLimit)
	cacheFile, metaFile := queryCachePaths(manager, key)
	if !isSafeCachePath(cacheFile) || !isSafeCachePath(metaFile) {
		fmt.Fprintf(os.Stderr, "fpf warning: unsafe cache path, refusing to write\n")
		return
	}
	cacheDir := filepath.Dir(cacheFile)
	if info, err := os.Lstat(cacheDir); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			fmt.Fprintf(os.Stderr, "fpf warning: cache dir %q is symlink, refusing to write\n", cacheDir)
			return
		}
	}
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "fpf warning: failed to create cache dir %q: %v\n", cacheDir, err)
		return
	}
	if info, err := os.Lstat(cacheDir); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			fmt.Fprintf(os.Stderr, "fpf warning: cache dir %q is symlink after creation, refusing to write\n", cacheDir)
			return
		}
	}

	var b strings.Builder
	for _, row := range rows {
		if row.Name == "" {
			continue
		}
		if len(row.Name) > maxPackageLength {
			continue
		}
		desc := row.Desc
		if desc == "" {
			desc = "-"
		}
		// Sanitize tabs/newlines in desc
		desc = strings.ReplaceAll(desc, "\n", " ")
		desc = strings.ReplaceAll(desc, "\r", " ")
		desc = strings.ReplaceAll(desc, "\t", " ")
		b.WriteString(row.Name)
		b.WriteString("\t")
		b.WriteString(desc)
		b.WriteString("\n")
	}
	content := b.String()
	if len(content) > 10<<20 {
		fmt.Fprintf(os.Stderr, "fpf warning: cache content too large (%d), skipping write\n", len(content))
		return
	}

	tmp, err := os.CreateTemp(filepath.Dir(cacheFile), "cache-*.tmp")
	if err != nil {
		fmt.Fprintf(os.Stderr, "fpf warning: failed to create temp cache file: %v\n", err)
		return
	}
	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		fmt.Fprintf(os.Stderr, "fpf warning: failed to write temp cache: %v\n", err)
		return
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		fmt.Fprintf(os.Stderr, "fpf warning: failed to close temp cache: %v\n", err)
		return
	}
	if err := os.Rename(tmp.Name(), cacheFile); err != nil {
		_ = os.Remove(tmp.Name())
		fmt.Fprintf(os.Stderr, "fpf warning: failed to rename cache file: %v\n", err)
		return
	}

	meta := strings.Builder{}
	now := time.Now()
	meta.WriteString("format_version=1\n")
	meta.WriteString("created_at=")
	meta.WriteString(now.UTC().Format(time.RFC3339))
	meta.WriteString("\n")
	meta.WriteString("created_epoch=")
	meta.WriteString(strconv.FormatInt(now.Unix(), 10))
	meta.WriteString("\n")
	meta.WriteString("fingerprint=")
	meta.WriteString(queryCacheFingerprint(manager, query, limit, npmLimit))
	meta.WriteString("\n")
	meta.WriteString("item_count=")
	meta.WriteString(strconv.Itoa(len(rows)))
	meta.WriteString("\n")
	tmpMeta, err := os.CreateTemp(filepath.Dir(metaFile), "meta-*.tmp")
	if err != nil {
		fmt.Fprintf(os.Stderr, "fpf warning: failed to create temp meta file: %v\n", err)
		return
	}
	if _, err := tmpMeta.WriteString(meta.String()); err != nil {
		_ = tmpMeta.Close()
		_ = os.Remove(tmpMeta.Name())
		fmt.Fprintf(os.Stderr, "fpf warning: failed to write temp meta: %v\n", err)
		return
	}
	if err := tmpMeta.Close(); err != nil {
		_ = os.Remove(tmpMeta.Name())
		fmt.Fprintf(os.Stderr, "fpf warning: failed to close temp meta: %v\n", err)
		return
	}
	if err := os.Rename(tmpMeta.Name(), metaFile); err != nil {
		_ = os.Remove(tmpMeta.Name())
		fmt.Fprintf(os.Stderr, "fpf warning: failed to rename meta file: %v\n", err)
	}
}

func binaryIdentityForManager(manager string) string {
	cmdPath, _ := exec.LookPath(managerCommandForFingerprint(manager))
	if cmdPath == "" {
		return "missing"
	}
	return binaryIdentity(cmdPath)
}

func binaryIdentity(cmdPath string) string {
	if cmdPath == "" || cmdPath == "missing" {
		return "missing"
	}
	// Resolve symlink to real path for stability
	realPath := cmdPath
	if resolved, err := filepath.EvalSymlinks(cmdPath); err == nil {
		realPath = resolved
	}
	info, err := os.Stat(realPath)
	if err != nil {
		return realPath
	}
	return fmt.Sprintf("%s|%d|%d", realPath, info.ModTime().Unix(), info.Size())
}

func queryCacheFingerprint(manager, query string, limit, npmLimit int) string {
	ident := binaryIdentityForManager(manager)
	// Bump fingerprint version to 4 to reflect expanded env capture and binary identity (mtime/size)
	return fmt.Sprintf("4|%s|%s|q=%s|limit=%d|npm=%d|qlim=%s|nqlim=%s|qper=%s|jsper=%s|npmper=%s|nonpm=%s",
		manager, ident, query, limit, npmLimit,
		os.Getenv("FPF_QUERY_RESULT_LIMIT"),
		os.Getenv("FPF_NO_QUERY_RESULT_LIMIT"),
		os.Getenv("FPF_QUERY_PER_MANAGER_LIMIT"),
		os.Getenv("FPF_JS_QUERY_PER_MANAGER_LIMIT"),
		os.Getenv("FPF_NPM_QUERY_PER_MANAGER_LIMIT"),
		os.Getenv("FPF_NO_QUERY_NPM_LIMIT"),
	)
}

func parseMetaMap(raw []byte) map[string]string {
	meta := make(map[string]string)
	for _, line := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		meta[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}
	return meta
}

func managerCommandForFingerprint(manager string) string {
	switch manager {
	case "apt":
		return "apt-cache"
	case "brew":
		return "brew"
	case "pacman":
		return "pacman"
	case "bun":
		return "bun"
	case "flatpak":
		return "flatpak"
	default:
		return manager
	}
}

func installedCacheEnabled() bool {
	setting := strings.ToLower(strings.TrimSpace(os.Getenv("FPF_DISABLE_INSTALLED_CACHE")))
	return !(setting == "1" || setting == "true" || setting == "yes" || setting == "on")
}

func installedCacheTTLSeconds() int {
	raw := strings.TrimSpace(os.Getenv("FPF_INSTALLED_CACHE_TTL"))
	if raw == "" {
		return 300
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < 0 {
		fmt.Fprintf(os.Stderr, "fpf warning: invalid FPF_INSTALLED_CACHE_TTL %q, using 300\n", raw)
		return 300
	}
	if v > 86400 {
		fmt.Fprintf(os.Stderr, "fpf warning: FPF_INSTALLED_CACHE_TTL %d exceeds max 86400, capping\n", v)
		return 86400
	}
	return v
}

func installedCachePaths(manager string) (string, string) {
	baseDir := filepath.Join(cacheRootPath(), "go-installed")
	return filepath.Join(baseDir, manager+".txt"), filepath.Join(baseDir, manager+".meta")
}

func installedFingerprint(manager string) string {
	ident := binaryIdentityForManager(manager)
	return manager + "|" + ident
}

func isValidManagerFormat(manager string) bool {
	if manager == "" || len(manager) > 32 || strings.Contains(manager, "\x00") {
		return false
	}
	// Allow lowercase alphanum and dash, as managers are normalized to lowercase
	for _, r := range manager {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			continue
		}
		return false
	}
	return true
}

func loadInstalledSetFromCache(manager string) (map[string]struct{}, bool) {
	if !isValidManagerFormat(manager) {
		return nil, false
	}
	if !installedCacheEnabled() {
		return nil, false
	}
	ttl := installedCacheTTLSeconds()
	if ttl <= 0 {
		return nil, false
	}
	cacheFile, metaFile := installedCachePaths(manager)
	if !isSafeCachePath(cacheFile) || !isSafeCachePath(metaFile) {
		return nil, false
	}
	if info, err := os.Lstat(cacheFile); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, false
	}
	if info, err := os.Lstat(metaFile); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, false
	}
	rawMeta, err := os.ReadFile(metaFile)
	if err != nil {
		return nil, false
	}
	if len(rawMeta) > 8192 {
		return nil, false
	}
	meta := parseMetaMap(rawMeta)
	createdEpoch, err := strconv.ParseInt(meta["created_epoch"], 10, 64)
	if err != nil {
		return nil, false
	}
	if createdEpoch <= 0 || createdEpoch > time.Now().Unix()+60 {
		return nil, false
	}
	if time.Now().Unix()-createdEpoch > int64(ttl) {
		return nil, false
	}
	if meta["fingerprint"] != installedFingerprint(manager) {
		return nil, false
	}

	raw, err := os.ReadFile(cacheFile)
	if err != nil {
		return nil, false
	}
	if len(raw) > 5<<20 {
		return nil, false
	}
	names := map[string]struct{}{}
	for _, line := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			if len(line) > maxPackageLength {
				continue
			}
			if !isValidPkgName(line) {
				continue
			}
			names[line] = struct{}{}
		}
	}
	if len(names) == 0 {
		return nil, false
	}
	return names, true
}

func storeInstalledSetToCache(manager string, names map[string]struct{}) {
	if !isValidManagerFormat(manager) {
		fmt.Fprintf(os.Stderr, "fpf warning: invalid manager for installed cache %q\n", manager)
		return
	}
	if !installedCacheEnabled() || len(names) == 0 {
		return
	}
	if len(names) > 10000 {
		fmt.Fprintf(os.Stderr, "fpf warning: installed set too large (%d), skipping cache\n", len(names))
		return
	}
	if override := strings.TrimSpace(os.Getenv("FPF_CACHE_DIR")); override != "" && !isSafeCachePath(override) {
		fmt.Fprintf(os.Stderr, "fpf warning: invalid FPF_CACHE_DIR %q, ignoring cache write\n", override)
		return
	}
	cacheFile, metaFile := installedCachePaths(manager)
	if !isSafeCachePath(cacheFile) || !isSafeCachePath(metaFile) {
		return
	}
	cacheDir := filepath.Dir(cacheFile)
	if info, err := os.Lstat(cacheDir); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			fmt.Fprintf(os.Stderr, "fpf warning: cache dir %q is symlink, refusing to write\n", cacheDir)
			return
		}
	}
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "fpf warning: failed to create installed cache dir %q: %v\n", cacheDir, err)
		return
	}
	if info, err := os.Lstat(cacheDir); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			fmt.Fprintf(os.Stderr, "fpf warning: cache dir %q is symlink after creation, refusing to write\n", cacheDir)
			return
		}
	}

	ordered := make([]string, 0, len(names))
	for name := range names {
		if len(name) > maxPackageLength || !isValidPkgName(name) {
			continue
		}
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)

	tmp, err := os.CreateTemp(filepath.Dir(cacheFile), "installed-*.tmp")
	if err != nil {
		fmt.Fprintf(os.Stderr, "fpf warning: failed to create temp installed cache: %v\n", err)
		return
	}
	for _, name := range ordered {
		if _, err := tmp.WriteString(name + "\n"); err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
			fmt.Fprintf(os.Stderr, "fpf warning: failed to write installed cache: %v\n", err)
			return
		}
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		fmt.Fprintf(os.Stderr, "fpf warning: failed to close installed cache: %v\n", err)
		return
	}
	if err := os.Rename(tmp.Name(), cacheFile); err != nil {
		_ = os.Remove(tmp.Name())
		fmt.Fprintf(os.Stderr, "fpf warning: failed to rename installed cache: %v\n", err)
		return
	}

	meta := strings.Builder{}
	now := time.Now()
	meta.WriteString("format_version=1\n")
	meta.WriteString("created_at=")
	meta.WriteString(now.UTC().Format(time.RFC3339))
	meta.WriteString("\n")
	meta.WriteString("created_epoch=")
	meta.WriteString(strconv.FormatInt(now.Unix(), 10))
	meta.WriteString("\n")
	meta.WriteString("fingerprint=")
	meta.WriteString(installedFingerprint(manager))
	meta.WriteString("\n")
	meta.WriteString("item_count=")
	meta.WriteString(strconv.Itoa(len(ordered)))
	meta.WriteString("\n")
	tmpMeta, err := os.CreateTemp(filepath.Dir(metaFile), "meta-*.tmp")
	if err != nil {
		fmt.Fprintf(os.Stderr, "fpf warning: failed to create temp installed meta: %v\n", err)
		return
	}
	if _, err := tmpMeta.WriteString(meta.String()); err != nil {
		_ = tmpMeta.Close()
		_ = os.Remove(tmpMeta.Name())
		fmt.Fprintf(os.Stderr, "fpf warning: failed to write installed meta: %v\n", err)
		return
	}
	if err := tmpMeta.Close(); err != nil {
		_ = os.Remove(tmpMeta.Name())
		fmt.Fprintf(os.Stderr, "fpf warning: failed to close installed meta: %v\n", err)
		return
	}
	if err := os.Rename(tmpMeta.Name(), metaFile); err != nil {
		_ = os.Remove(tmpMeta.Name())
		fmt.Fprintf(os.Stderr, "fpf warning: failed to rename installed meta: %v\n", err)
	}
}

func loadInstalledSet(manager string) map[string]struct{} {
	if names, ok := loadInstalledSetFromCache(manager); ok {
		return names
	}

	installed, err := executeInstalledEntries(installedInput{Manager: manager})
	if err != nil {
		return map[string]struct{}{}
	}

	names := make(map[string]struct{}, len(installed))
	for _, name := range installed {
		if name != "" {
			names[name] = struct{}{}
		}
	}

	storeInstalledSetToCache(manager, names)
	return names
}

func applyInstalledMarkers(query string, rows []buildDisplayRow, managers []string) []buildDisplayRow {
	if managers == nil {
		managers = []string{}
	}
	if rows == nil {
		rows = []buildDisplayRow{}
	}
	if strings.TrimSpace(os.Getenv("FPF_SKIP_INSTALLED_MARKERS")) == "1" {
		out := make([]buildDisplayRow, 0, len(rows))
		for _, row := range rows {
			row.Desc = "  " + row.Desc
			out = append(out, row)
		}
		return out
	}
	if skipNoQueryInstalledMarkers(query, managers) {
		out := make([]buildDisplayRow, 0, len(rows))
		for _, row := range rows {
			row.Desc = "  " + row.Desc
			out = append(out, row)
		}
		return out
	}

	type installedResult struct {
		manager string
		names   map[string]struct{}
	}

	// Validate managers and deduplicate
	validManagers := make([]string, 0, len(managers))
	seenManagers := map[string]struct{}{}
	for _, m := range managers {
		if !isManagerSupported(m) {
			continue
		}
		if _, ok := seenManagers[m]; ok {
			continue
		}
		seenManagers[m] = struct{}{}
		validManagers = append(validManagers, m)
	}
	if len(validManagers) == 0 {
		out := make([]buildDisplayRow, 0, len(rows))
		for _, row := range rows {
			row.Desc = "  " + row.Desc
			out = append(out, row)
		}
		return out
	}

	// Use mutex-protected map instead of channel to avoid close-race on timeout.
	installedMap := map[string]map[string]struct{}{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	// Limit parallelism similarly
	semSize := len(validManagers)
	if semSize > 6 {
		semSize = 6
	}
	sem := make(chan struct{}, semSize)
	for _, manager := range validManagers {
		wg.Add(1)
		sem <- struct{}{}
		go func(managerName string) {
			defer wg.Done()
			defer func() { <-sem }()
			defer func() {
				if r := recover(); r != nil {
					fmt.Fprintf(os.Stderr, "fpf warning: panic in installed set load for %s: %v\n", managerName, r)
					mu.Lock()
					installedMap[managerName] = map[string]struct{}{}
					mu.Unlock()
				}
			}()
			// Timeout for each manager's installed load
			done := make(chan map[string]struct{}, 1)
			go func() {
				done <- loadInstalledSet(managerName)
			}()
			select {
			case names := <-done:
				mu.Lock()
				installedMap[managerName] = names
				mu.Unlock()
			case <-time.After(10 * time.Second):
				fmt.Fprintf(os.Stderr, "fpf warning: timeout loading installed for %s\n", managerName)
				mu.Lock()
				installedMap[managerName] = map[string]struct{}{}
				mu.Unlock()
			}
		}(manager)
	}

	// Wait with overall timeout to prevent hanging forever
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		fmt.Fprintln(os.Stderr, "fpf warning: timeout waiting for installed markers")
		// Grace period for mutex writes to complete
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	}

	out := make([]buildDisplayRow, 0, len(rows))
	// Read under lock: straggler workers may still be writing after timeout.
	mu.Lock()
	for _, row := range rows {
		mark := "  "
		if managerSet, ok := installedMap[row.Manager]; ok {
			if _, installed := managerSet[row.Package]; installed {
				mark = "* "
			}
		}
		row.Desc = mark + row.Desc
		out = append(out, row)
	}
	mu.Unlock()

	return out
}

func skipNoQueryInstalledMarkers(query string, managers []string) bool {
	if strings.TrimSpace(query) != "" {
		return false
	}
	force := strings.ToLower(strings.TrimSpace(os.Getenv("FPF_NO_QUERY_INCLUDE_INSTALLED_MARKERS")))
	if force == "1" || force == "true" || force == "yes" || force == "on" {
		return false
	}
	return len(managers) > 1
}
