package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
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
	rows := collectManagerRows(input.Query, input.Managers)
	if len(rows) == 0 {
		return os.WriteFile(input.Output, []byte{}, 0o644)
	}

	merged := mergeDisplayRows(rows)
	marked := applyInstalledMarkers(merged, input.Managers)
	ranked := rankDisplayRows(input.Query, marked)
	limited := applyQueryLimit(input.Query, ranked)

	var b strings.Builder
	for _, row := range limited {
		b.WriteString(row.Manager)
		b.WriteString("\t")
		b.WriteString(row.Package)
		b.WriteString("\t")
		b.WriteString(row.Desc)
		b.WriteString("\n")
	}

	return os.WriteFile(input.Output, []byte(b.String()), 0o644)
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
	effectiveQuery, effectiveLimit, npmLimit := managerSearchConfig(manager, query)
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
			names := map[string]struct{}{}
			installed, err := executeInstalledEntries(installedInput{Manager: managerName})
			if err == nil {
				for _, name := range installed {
					if name != "" {
						names[name] = struct{}{}
					}
				}
			}
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
