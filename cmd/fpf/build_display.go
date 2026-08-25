package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
	if len(input.Output) > 4096 || strings.Contains(input.Output, "\x00") {
		return input, true, fmt.Errorf("invalid output path")
	}
	if !filepath.IsAbs(input.Output) {
		return input, true, fmt.Errorf("output must be absolute path: %q", input.Output)
	}
	if len(input.Managers) == 0 {
		return input, true, fmt.Errorf("--go-managers is required")
	}
	// Validate managers
	for _, m := range input.Managers {
		if err := validateManagerName(m); err != nil {
			return input, true, fmt.Errorf("invalid manager %q: %w", m, err)
		}
	}
	if err := validateQuery(input.Query); err != nil {
		return input, true, fmt.Errorf("invalid query: %w", err)
	}
	if len(input.Managers) > 20 {
		return input, true, fmt.Errorf("too many managers: %d", len(input.Managers))
	}

	return input, true, nil
}

func runBuildDisplay(input buildDisplayInput) error {
	if err := validateQuery(input.Query); err != nil {
		return fmt.Errorf("invalid query: %w", err)
	}
	rows, err := buildDisplayRows(input.Query, input.Managers)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		if err := os.MkdirAll(filepath.Dir(input.Output), 0o700); err != nil {
			return fmt.Errorf("failed to create output dir: %w", err)
		}
		return os.WriteFile(input.Output, []byte{}, 0o600)
	}

	if err := os.MkdirAll(filepath.Dir(input.Output), 0o700); err != nil {
		return fmt.Errorf("failed to create output dir: %w", err)
	}
	content := renderBuildDisplayRows(rows)
	if len(content) > 20<<20 {
		return fmt.Errorf("output too large: %d", len(content))
	}
	return os.WriteFile(input.Output, []byte(content), 0o600)
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
	marked := applyInstalledMarkers(query, merged, managers)
	logPerfTraceStage("mark", startMark)

	if limit := rankCandidateLimit(query); limit > 0 && len(marked) > limit {
		marked = capRowsForRanking(marked, limit)
	}

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
	if managers == nil || len(managers) == 0 {
		return nil
	}
	// Deduplicate and validate managers
	seen := map[string]struct{}{}
	filtered := make([]string, 0, len(managers))
	for _, m := range managers {
		if !isManagerSupported(m) {
			continue
		}
		if _, ok := seen[m]; ok {
			continue
		}
		seen[m] = struct{}{}
		filtered = append(filtered, m)
	}
	managers = filtered
	if len(managers) == 0 {
		return nil
	}
	hasNpm := false
	for _, manager := range managers {
		if manager == "npm" {
			hasNpm = true
			break
		}
	}

	// Use slice + mutex to avoid channel-close race on timeout.
	// Each goroutine writes to its index; final ordering preserves input manager order.
	ordered := make([][]buildDisplayRow, len(managers))
	var mu sync.Mutex
	var wg sync.WaitGroup
	// Limit parallelism to avoid spawning unbounded goroutines if managers list is large.
	// Simple semaphore with buffered channel size = min(8, len(managers))
	semSize := len(managers)
	if semSize > 8 {
		semSize = 8
	}
	sem := make(chan struct{}, semSize)
	for idx, manager := range managers {
		wg.Add(1)
		sem <- struct{}{}
		go func(index int, managerName string) {
			defer wg.Done()
			defer func() {
				<-sem
				if r := recover(); r != nil {
					fmt.Fprintf(os.Stderr, "fpf warning: panic in collectRowsForManager %s: %v\n", managerName, r)
				}
			}()
			rows := collectRowsForManager(managerName, query, len(managers), hasNpm)
			mu.Lock()
			if index >= 0 && index < len(ordered) {
				ordered[index] = rows
			}
			mu.Unlock()
		}(idx, manager)
	}

	// Wait with timeout to prevent hanging forever; use done channel.
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(45 * time.Second):
		fmt.Fprintln(os.Stderr, "fpf warning: timeout collecting manager rows")
		// Don't wait forever - but let goroutines finish in background; results already protected by mutex.
		// Wait a short grace period for buffered writes to complete.
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	}

	out := make([]buildDisplayRow, 0)
	// Read under lock: straggler goroutines may still be writing after timeout.
	mu.Lock()
	for _, mr := range ordered {
		if mr == nil {
			continue
		}
		out = append(out, mr...)
	}
	mu.Unlock()
	return out
}

func collectRowsForManager(manager string, query string, managerCount int, hasNpmManager bool) []buildDisplayRow {
	if err := validateManagerName(manager); err != nil {
		fmt.Fprintf(os.Stderr, "fpf warning: invalid manager %q: %v\n", manager, err)
		return nil
	}
	if err := validateQuery(query); err != nil {
		fmt.Fprintf(os.Stderr, "fpf warning: invalid query %q: %v\n", query, err)
		return nil
	}
	stageStart := time.Now()
	defer logPerfTraceStageDetail("search", manager, stageStart)

	effectiveQuery, effectiveLimit, npmLimit := managerSearchConfig(manager, query)
	if rows, ok := loadQueryRowsFromCache(manager, effectiveQuery, effectiveLimit, npmLimit); ok {
		return toBuildDisplayRows(manager, rows)
	}
	timeout := multiManagerSearchTimeout(manager, query, managerCount)
	if timeout <= 0 {
		timeout = defaultSearchTimeout
	}
	if timeout > 45*time.Second {
		timeout = 45 * time.Second
	}
	allowBunFallback := allowBunNpmFallback(manager, managerCount, hasNpmManager)

	rows, err := executeSearchEntries(searchInput{
		Manager:             manager,
		Query:               effectiveQuery,
		Limit:               effectiveLimit,
		NPMSearchLimit:      npmLimit,
		CommandTimeout:      timeout,
		AllowBunNPMFallback: allowBunFallback,
	})
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			fmt.Fprintf(os.Stderr, "fpf warning: search timeout for %s query %q\n", manager, effectiveQuery)
			return nil
		}
		fmt.Fprintf(os.Stderr, "fpf warning: search failed for %s: %v\n", manager, err)
		return nil
	}

	rows = dedupeRows(rows)
	if effectiveLimit > 0 && len(rows) > effectiveLimit {
		rows = rows[:effectiveLimit]
	}

	storeQueryRowsToCache(manager, effectiveQuery, effectiveLimit, npmLimit, rows)

	return toBuildDisplayRows(manager, rows)
}

func allowBunNpmFallback(manager string, managerCount int, hasNpmManager bool) bool {
	if manager != "bun" {
		return true
	}
	if managerCount <= 1 {
		return true
	}
	if override := strings.ToLower(strings.TrimSpace(os.Getenv("FPF_BUN_ALLOW_NPM_FALLBACK_MULTI"))); override == "1" || override == "true" || override == "yes" || override == "on" {
		return true
	}
	if !hasNpmManager {
		return true
	}
	return false
}

func multiManagerSearchTimeout(manager string, query string, managerCount int) time.Duration {
	if managerCount <= 1 {
		return 0
	}
	normalizedManager := strings.ToUpper(strings.ReplaceAll(manager, "-", "_"))
	if ms := parseEnvIntClamped("FPF_SEARCH_TIMEOUT_"+normalizedManager+"_MS", -1, 0, 600000); ms >= 0 {
		return time.Duration(ms) * time.Millisecond
	}
	if ms := parseEnvIntClamped("FPF_MULTI_MANAGER_SEARCH_TIMEOUT_MS", -1, 0, 600000); ms >= 0 {
		return time.Duration(ms) * time.Millisecond
	}

	switch manager {
	case "bun", "npm":
		if strings.TrimSpace(query) == "" {
			return 4000 * time.Millisecond
		}
		return 10000 * time.Millisecond
	case "flatpak":
		if strings.TrimSpace(query) == "" {
			return 15000 * time.Millisecond
		}
		return 10000 * time.Millisecond
	default:
		return 0
	}
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

func rankCandidateLimit(query string) int {
	if strings.TrimSpace(query) == "" {
		return 0
	}
	if parsed := parseEnvIntClamped("FPF_RANK_CANDIDATE_LIMIT", -1, 0, 100000); parsed >= 0 {
		return parsed
	}
	queryLimit := parseEnvInt("FPF_QUERY_RESULT_LIMIT", 0)
	if queryLimit > 0 {
		capLimit := queryLimit * 4
		if capLimit < 200 {
			capLimit = 200
		}
		return capLimit
	}
	return 400
}

func capRowsForRanking(rows []buildDisplayRow, limit int) []buildDisplayRow {
	if limit <= 0 || len(rows) <= limit {
		return rows
	}

	grouped := make(map[string][]buildDisplayRow)
	managerOrder := make([]string, 0)
	for _, row := range rows {
		if _, ok := grouped[row.Manager]; !ok {
			managerOrder = append(managerOrder, row.Manager)
		}
		grouped[row.Manager] = append(grouped[row.Manager], row)
	}

	indices := make(map[string]int, len(managerOrder))
	out := make([]buildDisplayRow, 0, limit)
	for len(out) < limit {
		progress := false
		for _, manager := range managerOrder {
			idx := indices[manager]
			managerRows := grouped[manager]
			if idx >= len(managerRows) {
				continue
			}
			out = append(out, managerRows[idx])
			indices[manager] = idx + 1
			progress = true
			if len(out) >= limit {
				break
			}
		}
		if !progress {
			break
		}
	}
	return out
}
