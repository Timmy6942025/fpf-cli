package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
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
	if len(input.OutputFile) > 4096 || strings.Contains(input.OutputFile, "\x00") {
		return input, true, fmt.Errorf("invalid output path")
	}
	if !filepath.IsAbs(input.OutputFile) {
		return input, true, fmt.Errorf("output must be absolute")
	}
	if input.SourceFile != "" {
		if len(input.SourceFile) > 4096 || strings.Contains(input.SourceFile, "\x00") {
			return input, true, fmt.Errorf("invalid source path")
		}
		if !filepath.IsAbs(input.SourceFile) {
			return input, true, fmt.Errorf("source must be absolute")
		}
	}

	return input, true, nil
}

func runMergeDisplay(input mergeInput) error {
	if input.SourceFile == "" {
		if err := os.MkdirAll(filepath.Dir(input.OutputFile), 0o700); err != nil {
			return fmt.Errorf("mkdir output dir: %w", err)
		}
		return os.WriteFile(input.OutputFile, []byte{}, 0o600)
	}
	if info, err := os.Lstat(input.SourceFile); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("source is symlink: %q", input.SourceFile)
	}
	if info, err := os.Lstat(input.OutputFile); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("output is symlink: %q", input.OutputFile)
	}

	raw, err := os.ReadFile(input.SourceFile)
	if err != nil || len(raw) == 0 {
		if err := os.MkdirAll(filepath.Dir(input.OutputFile), 0o700); err != nil {
			return fmt.Errorf("mkdir output dir: %w", err)
		}
		return os.WriteFile(input.OutputFile, []byte{}, 0o600)
	}
	if len(raw) > 20<<20 {
		return fmt.Errorf("source file too large: %d", len(raw))
	}

	rows := make([]mergeRow, 0)
	scanner := bufio.NewScanner(strings.NewReader(string(raw)))
	scanner.Buffer(make([]byte, 0, 64*1024), 1*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) > 8192 {
			continue
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 2 {
			continue
		}
		mgr := strings.TrimSpace(parts[0])
		pkg := strings.TrimSpace(parts[1])
		if mgr == "" || pkg == "" || len(mgr) > 32 || len(pkg) > maxPackageLength {
			continue
		}
		if !isManagerSupported(mgr) {
			continue
		}
		desc := "-"
		if len(parts) == 3 && parts[2] != "" {
			desc = strings.TrimSpace(parts[2])
			if desc == "" {
				desc = "-"
			}
		}
		rows = append(rows, mergeRow{Manager: mgr, Package: pkg, Desc: desc})
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "fpf warning: scan merge source: %v\n", err)
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
		if len(row.Manager) > 32 || len(row.Package) > maxPackageLength {
			continue
		}
		key := row.Manager + "\t" + row.Package
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		desc := row.Desc
		if desc == "" {
			desc = "-"
		}
		desc = strings.ReplaceAll(desc, "\n", " ")
		desc = strings.ReplaceAll(desc, "\r", " ")
		desc = strings.ReplaceAll(desc, "\t", " ")
		b.WriteString(row.Manager)
		b.WriteString("\t")
		b.WriteString(row.Package)
		b.WriteString("\t")
		b.WriteString(desc)
		b.WriteString("\n")
	}

	content := b.String()
	if len(content) > 20<<20 {
		return fmt.Errorf("output too large: %d", len(content))
	}
	if err := os.MkdirAll(filepath.Dir(input.OutputFile), 0o700); err != nil {
		return fmt.Errorf("mkdir output dir: %w", err)
	}
	if info, err := os.Lstat(input.OutputFile); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("output is symlink: %q", input.OutputFile)
	}
	return os.WriteFile(input.OutputFile, []byte(content), 0o600)
}
