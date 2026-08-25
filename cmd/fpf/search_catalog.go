package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// APT catalog functions
const (
	maxQueryLength   = 500
	maxPackageLength = 256
)

func validateQuery(query string) error {
	if len(query) > maxQueryLength {
		return fmt.Errorf("query too long: %d > %d", len(query), maxQueryLength)
	}
	if strings.Contains(query, "\x00") {
		return fmt.Errorf("query contains null byte")
	}
	return nil
}

func validateManagerName(manager string) error {
	if manager == "" {
		return fmt.Errorf("manager name is empty")
	}
	if len(manager) > 32 {
		return fmt.Errorf("manager name too long")
	}
	if !isManagerSupported(manager) {
		return fmt.Errorf("unsupported manager: %q", manager)
	}
	return nil
}

func loadAptCatalogRows(q string) ([]searchRow, error) {
	if err := validateQuery(q); err != nil {
		return nil, err
	}
	fingerprint := aptCatalogFingerprint()
	key := cacheChecksum(fingerprint)
	baseRoot := cacheRootPath()
	if baseRoot == "" {
		return nil, fmt.Errorf("cannot determine cache root")
	}
	cachePath := filepath.Join(baseRoot, "search-catalog", "apt", key+".tsv")

	// Try to load from cache
	if raw, err := os.ReadFile(cachePath); err == nil {
		rows := parseCachedRows(raw)
		if len(rows) > 0 {
			return filterAPT(rows, q), nil
		}
	}

	// Build catalog
	rows, err := buildAptCatalogRows()
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}

	// Cache the catalog - log but don't fail if cache write fails
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "fpf warning: failed to create apt catalog cache dir %q: %v\n", filepath.Dir(cachePath), err)
	} else if err := os.WriteFile(cachePath, []byte(renderAPT(rows)), 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "fpf warning: failed to write apt catalog cache %q: %v\n", cachePath, err)
	}

	return filterAPT(rows, q), nil
}

func aptCatalogFingerprint() string {
	cmdPath, _ := exec.LookPath("apt-cache")
	if cmdPath == "" {
		cmdPath = "missing"
	}
	if fixtureRoot := strings.TrimSpace(os.Getenv("FPF_TEST_FIXTURE_DIR")); fixtureRoot != "" {
		fixturePath := filepath.Join(fixtureRoot, "apt-dumpavail.txt")
		if info, err := os.Stat(fixturePath); err == nil {
			return fmt.Sprintf("apt|catalog|%s|fixture=%d|%d", cmdPath, info.ModTime().Unix(), info.Size())
		}
	}
	return fmt.Sprintf("apt|catalog|%s", cmdPath)
}

func cacheChecksum(input string) string {
	return stableChecksum(input)
}

func buildAptCatalogRows() ([]searchRow, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "apt-cache", "dumpavail")
	cmd.Env = os.Environ()
	cmd.Stderr = ioDiscard{}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("apt-cache dumpavail pipe failed: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("apt-cache dumpavail start failed: %w", err)
	}

	rows := parseAptDumpAvailReader(stdout)
	waitErr := cmd.Wait()
	if ctx.Err() == context.DeadlineExceeded {
		return nil, context.DeadlineExceeded
	}
	if waitErr != nil {
		return nil, fmt.Errorf("apt-cache dumpavail failed: %w", waitErr)
	}
	return rows, nil
}

func parseAptDumpAvail(out []byte) []searchRow {
	return parseAptDumpAvailReader(bytes.NewReader(out))
}

func parseAptDumpAvailReader(reader io.Reader) []searchRow {
	if reader == nil {
		return nil
	}
	rows := make([]searchRow, 0)
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1*1024*1024)
	var pkg, desc string
	flush := func() {
		name := strings.TrimSpace(pkg)
		if name == "" {
			return
		}
		if len(name) > maxPackageLength {
			return
		}
		descOut := strings.TrimSpace(desc)
		if descOut == "" {
			descOut = "-"
		}
		rows = append(rows, searchRow{Name: name, Desc: descOut})
	}
	for scanner.Scan() {
		line := scanner.Text()
		// Defensive: skip lines that exceed buffer to avoid panic
		if len(line) > 1*1024*1024 {
			continue
		}
		switch {
		case strings.HasPrefix(line, "Package:"):
			if pkg != "" {
				flush()
			}
			pkg = strings.TrimSpace(strings.TrimPrefix(line, "Package:"))
			desc = ""
		case strings.HasPrefix(line, "Description:"):
			desc = strings.TrimSpace(strings.TrimPrefix(line, "Description:"))
		case strings.TrimSpace(line) == "":
			if pkg != "" {
				flush()
				pkg = ""
				desc = ""
			}
		}
	}
	if pkg != "" {
		flush()
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "fpf warning: scan error in apt dump: %v\n", err)
	}
	return rows
}

func filterAPT(rows []searchRow, q string) []searchRow {
	if q == "" {
		return rows
	}
	qLower := strings.ToLower(q)
	filtered := make([]searchRow, 0)
	for _, row := range rows {
		if strings.Contains(strings.ToLower(row.Name), qLower) || strings.Contains(strings.ToLower(row.Desc), qLower) {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

func renderAPT(rows []searchRow) string {
	var b strings.Builder
	for _, row := range rows {
		b.WriteString(row.Name)
		b.WriteString("\t")
		b.WriteString(row.Desc)
		b.WriteString("\n")
	}
	return b.String()
}

func parseCachedRows(data []byte) []searchRow {
	rows := make([]searchRow, 0)
	for _, line := range splitLines(data) {
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) == 0 || parts[0] == "" {
			continue
		}
		name := parts[0]
		desc := "-"
		if len(parts) > 1 {
			desc = parts[1]
		}
		rows = append(rows, searchRow{Name: name, Desc: desc})
	}
	return rows
}

func loadBrewCatalogRows(q string) ([]searchRow, error) {
	if err := validateQuery(q); err != nil {
		return nil, err
	}
	baseRoot := cacheRootPath()
	if baseRoot == "" {
		return nil, fmt.Errorf("cannot determine cache root")
	}
	cachePath := filepath.Join(baseRoot, "catalog", "brew.tsv")
	metaPath := filepath.Join(baseRoot, "meta", "catalog", "brew.tsv.meta")
	fingerprint := brewCatalogFingerprint()

	if rawMeta, err := os.ReadFile(metaPath); err == nil {
		meta := parseMetaMap(rawMeta)
		if meta["fingerprint"] == fingerprint {
			if raw, err := os.ReadFile(cachePath); err == nil {
				rows := parseCachedRows(raw)
				if len(rows) > 0 {
					return filterBrewCatalog(rows, q), nil
				}
			}
		}
	}

	rows, err := buildBrewCatalogRows()
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}

	if err := os.MkdirAll(filepath.Dir(cachePath), 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "fpf warning: failed to create brew catalog cache dir %q: %v\n", filepath.Dir(cachePath), err)
		return filterBrewCatalog(rows, q), nil
	}
	if err := os.MkdirAll(filepath.Dir(metaPath), 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "fpf warning: failed to create brew catalog meta dir %q: %v\n", filepath.Dir(metaPath), err)
		return filterBrewCatalog(rows, q), nil
	}
	if err := os.WriteFile(cachePath, []byte(renderAPT(rows)), 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "fpf warning: failed to write brew catalog cache %q: %v\n", cachePath, err)
		return filterBrewCatalog(rows, q), nil
	}

	now := time.Now()
	meta := strings.Builder{}
	meta.WriteString("format_version=1\n")
	meta.WriteString("created_at=")
	meta.WriteString(now.UTC().Format(time.RFC3339))
	meta.WriteString("\n")
	meta.WriteString("created_epoch=")
	meta.WriteString(fmt.Sprintf("%d", now.Unix()))
	meta.WriteString("\n")
	meta.WriteString("fingerprint=")
	meta.WriteString(fingerprint)
	meta.WriteString("\n")
	meta.WriteString("item_count=")
	meta.WriteString(fmt.Sprintf("%d", len(rows)))
	meta.WriteString("\n")
	if err := os.WriteFile(metaPath, []byte(meta.String()), 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "fpf warning: failed to write brew catalog meta %q: %v\n", metaPath, err)
	}

	return filterBrewCatalog(rows, q), nil
}

func brewCatalogFingerprint() string {
	cmdPath, _ := exec.LookPath("brew")
	if cmdPath == "" {
		cmdPath = "missing"
	}
	return fmt.Sprintf("1|brew|%s", cmdPath)
}

func buildBrewCatalogRows() ([]searchRow, error) {
	rows := make([]searchRow, 0)
	err := runLineStreamQuietErr("brew", []string{"formulae"}, func(line string) {
		name := strings.TrimSpace(line)
		if name == "" {
			return
		}
		rows = append(rows, searchRow{Name: name, Desc: "-"})
	})
	if err != nil {
		return nil, err
	}

	err = runLineStreamQuietErr("brew", []string{"casks"}, func(line string) {
		name := strings.TrimSpace(line)
		if name == "" {
			return
		}
		rows = append(rows, searchRow{Name: name, Desc: "-"})
	})
	if err != nil {
		return nil, err
	}

	return dedupeRows(rows), nil
}

func filterBrewCatalog(rows []searchRow, q string) []searchRow {
	if q == "" {
		return rows
	}
	qLower := strings.ToLower(q)
	filtered := make([]searchRow, 0, len(rows))
	for _, row := range rows {
		if strings.Contains(strings.ToLower(row.Name), qLower) || strings.Contains(strings.ToLower(row.Desc), qLower) {
			filtered = append(filtered, row)
		}
	}
	return filtered
}
