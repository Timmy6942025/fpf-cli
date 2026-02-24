package main

import (
	"bufio"
	"os"
	"regexp"
	"sort"
	"strings"
)

type rankInput struct {
	Query string
	File  string
}

type rankRow struct {
	Manager string
	Package string
	Desc    string
}

type rankScore struct {
	Row            rankRow
	Score          int
	ManagerBias    int
	PkgTokenGap    int
	DescTokenGap   int
	PackageLen     int
	PackageLowered string
}

func maybeRunGoRankDisplay(args []string) (bool, int) {
	input, ok, err := parseRankInput(args)
	if !ok {
		return false, 0
	}
	if err != nil {
		return true, 2
	}

	if err := runRankDisplay(input); err != nil {
		return true, 1
	}

	return true, 0
}

func parseRankInput(args []string) (rankInput, bool, error) {
	input := rankInput{}
	hasMode := false

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--go-rank-display":
			hasMode = true
		case "--go-query":
			if i+1 < len(args) {
				input.Query = args[i+1]
				i++
			}
		case "--go-input":
			if i+1 < len(args) {
				input.File = args[i+1]
				i++
			}
		}
	}

	if !hasMode {
		return input, false, nil
	}
	if input.File == "" {
		return input, true, os.ErrInvalid
	}

	return input, true, nil
}

func runRankDisplay(input rankInput) error {
	query := strings.TrimSpace(input.Query)
	if query == "" {
		return nil
	}

	raw, err := os.ReadFile(input.File)
	if err != nil {
		return err
	}

	rows := parseRankRows(raw)
	if len(rows) == 0 {
		return nil
	}

	hasExact := false
	candidates := exactQueryCandidates(query)
	for _, row := range rows {
		pkgLower := strings.ToLower(row.Package)
		for _, cand := range candidates {
			if strings.EqualFold(pkgLower, strings.ToLower(cand)) {
				hasExact = true
				break
			}
		}
		if hasExact {
			break
		}
	}

	scored := scoreRows(query, hasExact, rows)
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

	var b strings.Builder
	for _, scoredRow := range scored {
		row := scoredRow.Row
		b.WriteString(row.Manager)
		b.WriteString("\t")
		b.WriteString(row.Package)
		b.WriteString("\t")
		if row.Desc == "" {
			b.WriteString("-")
		} else {
			b.WriteString(row.Desc)
		}
		b.WriteString("\n")
	}

	return os.WriteFile(input.File, []byte(b.String()), 0o644)
}

func parseRankRows(raw []byte) []rankRow {
	rows := make([]rankRow, 0)
	scanner := bufio.NewScanner(strings.NewReader(string(raw)))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 2 {
			continue
		}
		row := rankRow{Manager: parts[0], Package: parts[1], Desc: "-"}
		if len(parts) == 3 && parts[2] != "" {
			row.Desc = parts[2]
		}
		rows = append(rows, row)
	}
	return rows
}

func exactQueryCandidates(query string) []string {
	fields := strings.Fields(query)
	if len(fields) == 0 {
		return nil
	}

	compact := strings.Join(fields, " ")
	candidates := []string{compact}
	if strings.Contains(compact, " ") {
		candidates = append(candidates,
			strings.ReplaceAll(compact, " ", "-"),
			strings.ReplaceAll(compact, " ", "_"),
			strings.ReplaceAll(compact, " ", ""),
		)
	}

	unique := make([]string, 0, len(candidates))
	seen := map[string]struct{}{}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		unique = append(unique, candidate)
	}

	return unique
}

func scoreRows(query string, hasExact bool, rows []rankRow) []rankScore {
	q := strings.ToLower(strings.TrimSpace(query))
	qNorm := normalizeAlphaNum(q)
	queryTokens := splitAlphaNumTokens(q)
	queryTokenCount := len(queryTokens)
	penaltyRe := regexp.MustCompile(`(plugin|template|starter|boilerplate|router|hooks?|mcp|integration)`)

	scored := make([]rankScore, 0, len(rows))
	for _, row := range rows {
		mgr := strings.ToLower(row.Manager)
		pkg := row.Package
		desc := row.Desc

		pkgLower := strings.ToLower(pkg)
		descLower := strings.ToLower(desc)
		pkgNorm := normalizeAlphaNum(pkgLower)
		descNorm := normalizeAlphaNum(descLower)

		pkgTokenHits := 0
		descTokenHits := 0
		for _, token := range queryTokens {
			if strings.Contains(pkgLower, token) || strings.Contains(pkgNorm, token) {
				pkgTokenHits++
			}
			if strings.Contains(descLower, token) || strings.Contains(descNorm, token) {
				descTokenHits++
			}
		}

		pkgTokenCount := len(splitAlphaNumTokens(pkgLower))

		score := 12
		if q != "" {
			switch {
			case strings.EqualFold(pkgLower, q) || (qNorm != "" && pkgNorm == qNorm):
				score = 0
			case qNorm != "" && strings.HasPrefix(pkgNorm, qNorm):
				score = 1
			case queryTokenCount > 1 && pkgTokenHits == queryTokenCount:
				score = 2
			case qNorm != "" && strings.Contains(pkgNorm, qNorm):
				score = 3
			case queryTokenCount > 0 && pkgTokenHits > 0:
				score = 4
			case queryTokenCount > 0 && descTokenHits == queryTokenCount:
				score = 5
			case queryTokenCount > 0 && descTokenHits > 0:
				score = 6
			}
		}

		if q != "" && hasExact && pkgTokenHits == queryTokenCount && queryTokenCount > 0 && pkgTokenCount > queryTokenCount {
			score += 5
		}

		if q != "" && penaltyRe.MatchString(descLower) {
			score += 2
		}

		managerBias := 0
		switch mgr {
		case "npm":
			managerBias = 4
		case "bun":
			managerBias = 3
		}

		scored = append(scored, rankScore{
			Row:            row,
			Score:          score,
			ManagerBias:    managerBias,
			PkgTokenGap:    99 - pkgTokenHits,
			DescTokenGap:   99 - descTokenHits,
			PackageLen:     len(pkg),
			PackageLowered: pkgLower,
		})
	}

	return scored
}

func normalizeAlphaNum(value string) string {
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func splitAlphaNumTokens(value string) []string {
	tokens := make([]string, 0)
	var b strings.Builder
	flush := func() {
		if b.Len() == 0 {
			return
		}
		tokens = append(tokens, b.String())
		b.Reset()
	}

	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			continue
		}
		flush()
	}
	flush()

	return tokens
}
