package main

import (
	"bufio"
	"os"
	"sort"
	"strings"
)

type mergeInput struct {
	SourceFile string
	OutputFile string
}

type mergeRow struct {
	Manager string
	Package string
	Desc    string
}

func maybeRunGoMergeDisplay(args []string) (bool, int) {
	input, ok, err := parseMergeInput(args)
	if !ok {
		return false, 0
	}
	if err != nil {
		return true, 2
	}

	if err := runMergeDisplay(input); err != nil {
		return true, 1
	}

	return true, 0
}

func parseMergeInput(args []string) (mergeInput, bool, error) {
	input := mergeInput{}
	hasMode := false

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--go-merge-display":
			hasMode = true
		case "--go-source":
			if i+1 < len(args) {
				input.SourceFile = args[i+1]
				i++
			}
		case "--go-output":
			if i+1 < len(args) {
				input.OutputFile = args[i+1]
				i++
			}
		}
	}

	if !hasMode {
		return input, false, nil
	}
	if input.OutputFile == "" {
		return input, true, os.ErrInvalid
	}

	return input, true, nil
}

func runMergeDisplay(input mergeInput) error {
	if input.SourceFile == "" {
		return os.WriteFile(input.OutputFile, []byte{}, 0o644)
	}

	raw, err := os.ReadFile(input.SourceFile)
	if err != nil || len(raw) == 0 {
		return os.WriteFile(input.OutputFile, []byte{}, 0o644)
	}

	rows := make([]mergeRow, 0)
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
		mgr := parts[0]
		pkg := parts[1]
		desc := "-"
		if len(parts) == 3 && parts[2] != "" {
			desc = parts[2]
		}
		rows = append(rows, mergeRow{Manager: mgr, Package: pkg, Desc: desc})
	}

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
	var b strings.Builder
	for _, row := range rows {
		if row.Manager == "" || row.Package == "" {
			continue
		}
		key := row.Manager + "\t" + row.Package
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
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

	return os.WriteFile(input.OutputFile, []byte(b.String()), 0o644)
}
