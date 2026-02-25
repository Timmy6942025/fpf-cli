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

type buildDisplayInput struct {
	Query    string
	Output   string
	Managers []string
}

type buildDisplayRow struct {
	Manager string
	Package string
	Desc    string
}

func maybeRunGoBuildDisplay(args []string) (bool, int) {
	input, ok, err := parseBuildDisplayInput(args)
	if !ok {
		return false, 0
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "fpf-go: %v\n", err)
		return true, 2
	}

	if err := runBuildDisplay(input); err != nil {
		fmt.Fprintf(os.Stderr, "fpf-go: %v\n", err)
		return true, 1
	}

	return true, 0
}

func parseBuildDisplayInput(args []string) (buildDisplayInput, bool, error) {
	input := buildDisplayInput{}
	hasMode := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--go-build-display":
			hasMode = true
		case "--go-query":
			if i+1 < len(args) {
				input.Query = args[i+1]
				i++
			}
		case "--go-output":
			if i+1 < len(args) {
				input.Output = args[i+1]
				i++
			}
		case "--go-managers":
			if i+1 < len(args) {
				input.Managers = splitManagerArg(args[i+1])
				i++
			}
		}
	}

	if !hasMode {
		return input, false, nil
	}
	if input.Output == "" {
		return input, true, os.ErrInvalid
	}
	if len(input.Managers) == 0 {
		return input, true, fmt.Errorf("--go-managers is required")
	}

	return input, true, nil
}

func runBuildDisplay(input buildDisplayInput) error {
	rows, err := buildDisplayRows(input.Query, input.Managers)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return os.WriteFile(input.Output, []byte{}, 0o644)
	}

	return os.WriteFile(input.Output, []byte(renderBuildDisplayRows(rows)), 0o644)
}

func buildDisplayRows(query string, managers []string) ([]buildDisplayRow, error) {
	rows := collectManagerRows(query, managers)
	if len(rows) == 0 {
		return nil, nil
	}
	return processDisplayRows(query, managers, rows), nil
}

func processDisplayRows(query string, managers []string, rows []buildDisplayRow) []buildDisplayRow {
	startMerge := time.Now()
	merged := mergeDisplayRows(rows)
	logPerfTraceStage("merge", startMerge)

	startMark := time.Now()
	marked := applyInstalledMarkers(merged, managers)
	logPerfTraceStage("mark", startMark)

	startRank := time.Now()
	ranked := rankDisplayRows(query, marked)
	logPerfTraceStage("rank", startRank)

	startLimit := time.Now()
	limited := applyQueryLimit(query, ranked)
	logPerfTraceStage("limit", startLimit)

	return limited
}

func renderBuildDisplayRows(rows []buildDisplayRow) string {
	var b strings.Builder
	for _, row := range rows {
		b.WriteString(row.Manager)
		b.WriteString("\t")
		b.WriteString(row.Package)
		b.WriteString("\t")
		b.WriteString(row.Desc)
		b.WriteString("\n")
	}
	return b.String()
}

func collectManagerRows(query string, managers []string) []buildDisplayRow {
	type managerRows struct {
		index int
		rows  []buildDisplayRow
	}

	ch := make(chan managerRows, len(managers))
	var wg sync.WaitGroup
	for idx, manager := range managers {
		wg.Add(1)
		go func(index int, managerName string) {
			defer wg.Done()
			rows := collectRowsForManager(managerName, query)
			ch <- managerRows{index: index, rows: rows}
		}(idx, manager)
	}

	wg.Wait()
	close(ch)

	ordered := make([][]buildDisplayRow, len(managers))
	for item := range ch {
		ordered[item.index] = item.rows
	}

	out := make([]buildDisplayRow, 0)
	for _, managerRows := range ordered {
		out = append(out, managerRows...)
	}
	return out
}

func collectRowsForManager(manager string, query string) []buildDisplayRow {
	stageStart := time.Now()
	defer logPerfTraceStageDetail("search", manager, stageStart)

	effectiveQuery, effectiveLimit, npmLimit := managerSearchConfig(manager, query)
	if rows, ok := loadQueryRowsFromCache(manager, effectiveQuery, effectiveLimit, npmLimit); ok {
		return toBuildDisplayRows(manager, rows)
	}

	rows, err := executeSearchEntries(searchInput{
		Manager:        manager,
		Query:          effectiveQuery,
		Limit:          effectiveLimit,
		NPMSearchLimit: npmLimit,
	})
	if err != nil {
		return nil
	}

	rows = dedupeRows(rows)
	if effectiveLimit > 0 && len(rows) > effectiveLimit {
		rows = rows[:effectiveLimit]
	}

	storeQueryRowsToCache(manager, effectiveQuery, effectiveLimit, npmLimit, rows)

	return toBuildDisplayRows(manager, rows)
}

func toBuildDisplayRows(manager string, rows []searchRow) []buildDisplayRow {
	out := make([]buildDisplayRow, 0, len(rows))
	for _, row := range rows {
		if row.Name == "" {
			continue
		}
		desc := row.Desc
		if desc == "" {
			desc = "-"
		}
		out = append(out, buildDisplayRow{Manager: manager, Package: row.Name, Desc: desc})
	}

	return out
}

func cacheRootPath() string {
	if override := strings.TrimSpace(os.Getenv("FPF_CACHE_DIR")); override != "" {
		return override
	}

	if runtime.GOOS == "windows" {
		if local := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); local != "" {
			return filepath.Join(local, "fpf")
		}
		if app := strings.TrimSpace(os.Getenv("APPDATA")); app != "" {
			return filepath.Join(app, "fpf")
		}
	}

	if xdg := strings.TrimSpace(os.Getenv("XDG_CACHE_HOME")); xdg != "" {
		return filepath.Join(xdg, "fpf")
	}
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		return filepath.Join(home, ".cache", "fpf")
	}

	return filepath.Join(os.TempDir(), "fpf-cache")
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

	if raw := strings.TrimSpace(os.Getenv("FPF_QUERY_CACHE_TTL")); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v >= 0 {
			base = v
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
				base = v
			}
		}
	}

	return base
}

func queryCacheKey(manager, query string, limit, npmLimit int) string {
	payload := fmt.Sprintf("v2|mgr=%s|q=%s|limit=%d|npm=%d|qlim=%s|nqlim=%s", manager, query, limit, npmLimit, os.Getenv("FPF_QUERY_RESULT_LIMIT"), os.Getenv("FPF_NO_QUERY_RESULT_LIMIT"))
	return fmt.Sprintf("%08x", crc32.ChecksumIEEE([]byte(payload)))
}

func queryCachePaths(manager, key string) (string, string) {
	baseDir := filepath.Join(cacheRootPath(), "go-query", manager)
	return filepath.Join(baseDir, key+".tsv"), filepath.Join(baseDir, key+".meta")
}

func loadQueryRowsFromCache(manager, query string, limit, npmLimit int) ([]searchRow, bool) {
	if !queryCacheEnabledForManager(manager) {
		return nil, false
	}

	ttl := queryCacheTTLSeconds(manager)
	if ttl <= 0 {
		return nil, false
	}

	key := queryCacheKey(manager, query, limit, npmLimit)
	cacheFile, metaFile := queryCachePaths(manager, key)

	rawMeta, err := os.ReadFile(metaFile)
	if err != nil {
		return nil, false
	}
	meta := parseMetaMap(rawMeta)
	createdEpoch, err := strconv.ParseInt(meta["created_epoch"], 10, 64)
	if err != nil {
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

	rows := make([]searchRow, 0)
	for _, line := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) == 0 || parts[0] == "" {
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
	if !queryCacheEnabledForManager(manager) {
		return
	}
	if len(rows) == 0 {
		return
	}
	ttl := queryCacheTTLSeconds(manager)
	if ttl <= 0 {
		return
	}

	key := queryCacheKey(manager, query, limit, npmLimit)
	cacheFile, metaFile := queryCachePaths(manager, key)
	if err := os.MkdirAll(filepath.Dir(cacheFile), 0o755); err != nil {
		return
	}

	var b strings.Builder
	for _, row := range rows {
		if row.Name == "" {
			continue
		}
		desc := row.Desc
		if desc == "" {
			desc = "-"
		}
		b.WriteString(row.Name)
		b.WriteString("\t")
		b.WriteString(desc)
		b.WriteString("\n")
	}

	tmp, err := os.CreateTemp(filepath.Dir(cacheFile), "cache-*.tmp")
	if err != nil {
		return
	}
	_, _ = tmp.WriteString(b.String())
	_ = tmp.Close()
	if err := os.Rename(tmp.Name(), cacheFile); err != nil {
		_ = os.Remove(tmp.Name())
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
		return
	}
	_, _ = tmpMeta.WriteString(meta.String())
	_ = tmpMeta.Close()
	if err := os.Rename(tmpMeta.Name(), metaFile); err != nil {
		_ = os.Remove(tmpMeta.Name())
	}
}

func queryCacheFingerprint(manager, query string, limit, npmLimit int) string {
	cmdPath, _ := exec.LookPath(managerCommandForFingerprint(manager))
	if cmdPath == "" {
		cmdPath = "missing"
	}
	return fmt.Sprintf("2|%s|%s|q=%s|limit=%d|npm=%d|qlim=%s|nqlim=%s", manager, cmdPath, query, limit, npmLimit, os.Getenv("FPF_QUERY_RESULT_LIMIT"), os.Getenv("FPF_NO_QUERY_RESULT_LIMIT"))
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

func managerSearchConfig(manager string, query string) (string, int, int) {
	npmLimit := parseEnvInt("FPF_NO_QUERY_NPM_LIMIT", 120)
	if npmLimit <= 0 {
		npmLimit = 500
	}

	lineLimit := parseEnvInt("FPF_NO_QUERY_RESULT_LIMIT", 120)
	if lineLimit < 0 {
		lineLimit = 0
	}

	queryLimit := parseEnvInt("FPF_QUERY_PER_MANAGER_LIMIT", 40)
	if queryLimit <= 0 {
		queryLimit = 40
	}
	if query != "" && (manager == "npm" || manager == "bun") {
		queryLimit = parseEnvInt("FPF_JS_QUERY_PER_MANAGER_LIMIT", 200)
		if queryLimit <= 0 {
			queryLimit = parseEnvInt("FPF_NPM_QUERY_PER_MANAGER_LIMIT", 200)
		}
		if queryLimit <= 0 {
			queryLimit = 200
		}
	}

	effectiveLimit := queryLimit
	effectiveQuery := query
	if query == "" {
		if lineLimit > 0 {
			effectiveLimit = lineLimit
		}
		switch manager {
		case "apt", "dnf", "pacman", "zypper", "emerge", "choco", "scoop", "snap":
			effectiveQuery = "a"
		case "brew", "npm", "bun", "winget":
			effectiveQuery = "aa"
		}
	}

	return effectiveQuery, effectiveLimit, npmLimit
}

func mergeDisplayRows(rows []buildDisplayRow) []buildDisplayRow {
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Manager != rows[j].Manager {
			return rows[i].Manager < rows[j].Manager
		}
		if rows[i].Package != rows[j].Package {
			return rows[i].Package < rows[j].Package
		}
		return rows[i].Desc < rows[j].Desc
	})

	seen := map[string]struct{}{}
	out := make([]buildDisplayRow, 0, len(rows))
	for _, row := range rows {
		if row.Manager == "" || row.Package == "" {
			continue
		}
		key := row.Manager + "\t" + row.Package
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		if row.Desc == "" {
			row.Desc = "-"
		}
		out = append(out, row)
	}
	return out
}

func applyInstalledMarkers(rows []buildDisplayRow, managers []string) []buildDisplayRow {
	if strings.TrimSpace(os.Getenv("FPF_SKIP_INSTALLED_MARKERS")) == "1" {
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

	ch := make(chan installedResult, len(managers))
	var wg sync.WaitGroup
	seenManagers := map[string]struct{}{}
	for _, manager := range managers {
		if _, ok := seenManagers[manager]; ok {
			continue
		}
		seenManagers[manager] = struct{}{}

		wg.Add(1)
		go func(managerName string) {
			defer wg.Done()
			names := loadInstalledSet(managerName)
			ch <- installedResult{manager: managerName, names: names}
		}(manager)
	}

	wg.Wait()
	close(ch)

	installedMap := map[string]map[string]struct{}{}
	for result := range ch {
		installedMap[result.manager] = result.names
	}

	out := make([]buildDisplayRow, 0, len(rows))
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

	return out
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
		return 300
	}
	return v
}

func installedCachePaths(manager string) (string, string) {
	baseDir := filepath.Join(cacheRootPath(), "go-installed")
	return filepath.Join(baseDir, manager+".txt"), filepath.Join(baseDir, manager+".meta")
}

func installedFingerprint(manager string) string {
	cmd, _ := exec.LookPath(managerCommandForFingerprint(manager))
	return manager + "|" + cmd
}

func loadInstalledSetFromCache(manager string) (map[string]struct{}, bool) {
	if !installedCacheEnabled() {
		return nil, false
	}
	ttl := installedCacheTTLSeconds()
	if ttl <= 0 {
		return nil, false
	}
	cacheFile, metaFile := installedCachePaths(manager)
	rawMeta, err := os.ReadFile(metaFile)
	if err != nil {
		return nil, false
	}
	meta := parseMetaMap(rawMeta)
	createdEpoch, err := strconv.ParseInt(meta["created_epoch"], 10, 64)
	if err != nil {
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
	names := map[string]struct{}{}
	for _, line := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			names[line] = struct{}{}
		}
	}
	if len(names) == 0 {
		return nil, false
	}
	return names, true
}

func storeInstalledSetToCache(manager string, names map[string]struct{}) {
	if !installedCacheEnabled() || len(names) == 0 {
		return
	}
	cacheFile, metaFile := installedCachePaths(manager)
	if err := os.MkdirAll(filepath.Dir(cacheFile), 0o755); err != nil {
		return
	}

	ordered := make([]string, 0, len(names))
	for name := range names {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)

	tmp, err := os.CreateTemp(filepath.Dir(cacheFile), "installed-*.tmp")
	if err != nil {
		return
	}
	for _, name := range ordered {
		_, _ = tmp.WriteString(name + "\n")
	}
	_ = tmp.Close()
	if err := os.Rename(tmp.Name(), cacheFile); err != nil {
		_ = os.Remove(tmp.Name())
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
		return
	}
	_, _ = tmpMeta.WriteString(meta.String())
	_ = tmpMeta.Close()
	if err := os.Rename(tmpMeta.Name(), metaFile); err != nil {
		_ = os.Remove(tmpMeta.Name())
	}
}

func rankDisplayRows(query string, rows []buildDisplayRow) []buildDisplayRow {
	if strings.TrimSpace(query) == "" || len(rows) == 0 {
		return rows
	}

	rankRows := make([]rankRow, 0, len(rows))
	for _, row := range rows {
		rankRows = append(rankRows, rankRow{Manager: row.Manager, Package: row.Package, Desc: row.Desc})
	}

	hasExact := false
	for _, row := range rankRows {
		pkgLower := strings.ToLower(row.Package)
		for _, cand := range exactQueryCandidates(query) {
			if strings.EqualFold(pkgLower, strings.ToLower(cand)) {
				hasExact = true
				break
			}
		}
		if hasExact {
			break
		}
	}

	scored := scoreRows(query, hasExact, rankRows)
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].Score != scored[j].Score {
			return scored[i].Score < scored[j].Score
		}
		if scored[i].ManagerBias != scored[j].ManagerBias {
			return scored[i].ManagerBias < scored[j].ManagerBias
		}
		if scored[i].PkgTokenGap != scored[j].PkgTokenGap {
			return scored[i].PkgTokenGap < scored[j].PkgTokenGap
		}
		if scored[i].DescTokenGap != scored[j].DescTokenGap {
			return scored[i].DescTokenGap < scored[j].DescTokenGap
		}
		if scored[i].PackageLen != scored[j].PackageLen {
			return scored[i].PackageLen < scored[j].PackageLen
		}
		return scored[i].PackageLowered < scored[j].PackageLowered
	})

	out := make([]buildDisplayRow, 0, len(scored))
	for _, item := range scored {
		out = append(out, buildDisplayRow{Manager: item.Row.Manager, Package: item.Row.Package, Desc: item.Row.Desc})
	}
	return out
}

func applyQueryLimit(query string, rows []buildDisplayRow) []buildDisplayRow {
	if strings.TrimSpace(query) == "" {
		return rows
	}
	queryLimit := parseEnvInt("FPF_QUERY_RESULT_LIMIT", 0)
	if queryLimit <= 0 || len(rows) <= queryLimit {
		return rows
	}
	return rows[:queryLimit]
}
