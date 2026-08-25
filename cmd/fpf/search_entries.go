package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/Timmy6942025/fpf-cli/internal/flatpak"
)

type searchInput struct {
	Manager             string
	Query               string
	Limit               int
	NPMSearchLimit      int
	CommandTimeout      time.Duration
	AllowBunNPMFallback bool
}

func maybeRunGoSearchEntries(args []string) (bool, int) {
	input, ok, err := parseSearchInput(args)
	if !ok {
		return false, 0
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "fpf-go: %v\n", err)
		return true, 2
	}

	rows, err := executeSearchEntries(input)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fpf-go: search failed for %s: %v\n", input.Manager, err)
		return true, 1
	}

	rows = dedupeRows(rows)
	if input.Limit > 0 && len(rows) > input.Limit {
		rows = rows[:input.Limit]
	}

	for _, row := range rows {
		if row.Name == "" {
			continue
		}
		desc := row.Desc
		if desc == "" {
			desc = "-"
		}
		fmt.Printf("%s\t%s\n", row.Name, desc)
	}

	return true, 0
}

func parseSearchInput(args []string) (searchInput, bool, error) {
	input := searchInput{NPMSearchLimit: 500, AllowBunNPMFallback: true}
	if len(args) == 0 {
		return input, false, nil
	}

	hasFlag := false
	modeEnabled := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			break
		}

		switch arg {
		case "--go-search-entries":
			hasFlag = true
			modeEnabled = true
		case "--go-manager":
			if !modeEnabled {
				continue
			}
			hasFlag = true
			if i+1 >= len(args) {
				return input, true, errors.New("missing value for --go-manager")
			}
			input.Manager = normalizeManagerName(args[i+1])
			i++
		case "--go-query":
			if !modeEnabled {
				continue
			}
			hasFlag = true
			if i+1 >= len(args) {
				return input, true, errors.New("missing value for --go-query")
			}
			input.Query = args[i+1]
			i++
		case "--go-limit":
			if !modeEnabled {
				continue
			}
			hasFlag = true
			if i+1 >= len(args) {
				return input, true, errors.New("missing value for --go-limit")
			}
			input.Limit = clampInt(parseStrictInt(args[i+1]), 0, 1000)
			i++
		case "--go-npm-search-limit":
			if !modeEnabled {
				continue
			}
			hasFlag = true
			if i+1 >= len(args) {
				return input, true, errors.New("missing value for --go-npm-search-limit")
			}
			input.NPMSearchLimit = clampInt(parseStrictInt(args[i+1]), 0, 1000)
			i++
		}
	}

	if !modeEnabled || !hasFlag {
		return input, false, nil
	}
	if input.Manager == "" {
		return input, true, errors.New("--go-manager is required")
	}
	if err := validateManagerName(input.Manager); err != nil {
		return input, true, err
	}
	if err := validateQuery(input.Query); err != nil {
		return input, true, fmt.Errorf("invalid query: %w", err)
	}
	if input.Limit < 0 {
		input.Limit = 0
	}
	if input.Limit > 1000 {
		input.Limit = 1000
	}
	if input.NPMSearchLimit <= 0 {
		input.NPMSearchLimit = 500
	}
	if input.NPMSearchLimit > 1000 {
		input.NPMSearchLimit = 1000
	}
	if input.CommandTimeout < 0 {
		input.CommandTimeout = 0
	}

	return input, true, nil
}

type searchRow struct {
	Name string
	Desc string
}

func executeSearchEntries(input searchInput) ([]searchRow, error) {
	manager := input.Manager
	query := input.Query
	if err := validateManagerName(manager); err != nil {
		return nil, err
	}
	if err := validateQuery(query); err != nil {
		return nil, fmt.Errorf("invalid query: %w", err)
	}
	// Ensure we have a sane timeout even when caller passes 0 (single-manager search)
	effectiveTimeout := input.CommandTimeout
	if effectiveTimeout <= 0 {
		effectiveTimeout = 30 * time.Second
	}
	// Cap timeout to avoid hanging too long
	if effectiveTimeout > 120*time.Second {
		effectiveTimeout = 120 * time.Second
	}
	runOutput := func(name string, args ...string) ([]byte, error) {
		return runOutputQuietErrWithTimeout(effectiveTimeout, name, args...)
	}

	switch manager {
	case "apt":
		// Use catalog-based search for better performance
		if catalogRows, err := loadAptCatalogRows(query); err == nil && len(catalogRows) > 0 {
			return catalogRows, nil
		}
		// Fallback to direct search if catalog fails or is empty
		out, err := runOutput("apt-cache", "search", "--", query)
		if err != nil {
			return nil, err
		}
		return parseAptSearch(out), nil
	case "dnf":
		pattern := "*"
		if query != "" {
			pattern = "*" + query + "*"
		}
		out, err := runOutput("dnf", "-q", "list", "available", pattern)
		if err != nil {
			return nil, err
		}
		return parseDNFSearch(out), nil
	case "pacman":
		out, err := runOutput("pacman", "-Ss", "--", query)
		if err != nil {
			return nil, err
		}
		return parsePacmanSearch(out), nil
	case "zypper":
		out, err := runOutput("zypper", "--non-interactive", "--quiet", "search", "--details", "--type", "package", query)
		if err != nil {
			return nil, err
		}
		return parseZypperSearch(out), nil
	case "emerge":
		out, err := runOutput("emerge", "--searchdesc", "--color=n", query)
		if err != nil {
			return nil, err
		}
		return parseEmergeSearch(out), nil
	case "brew":
		if catalogRows, err := loadBrewCatalogRows(query); err == nil && len(catalogRows) > 0 {
			return catalogRows, nil
		}
		out, err := runOutput("brew", "search", query)
		if err != nil {
			return nil, err
		}
		return parseBrewSearch(out), nil
	case "winget":
		out, err := runOutput("winget", "search", query, "--source", "winget", "--accept-source-agreements", "--disable-interactivity")
		if err != nil {
			return nil, err
		}
		return parseWingetSearch(out), nil
	case "choco":
		out, err := runOutput("choco", "search", query, "--limit-output")
		if err != nil {
			return nil, err
		}
		return parseChocoSearch(out), nil
	case "scoop":
		out, err := runOutput("scoop", "search", query)
		if err != nil {
			return nil, err
		}
		return parseScoopSearch(out), nil
	case "snap":
		out, err := runOutput("snap", "find", query)
		if err != nil {
			return nil, err
		}
		return parseSnapSearch(out), nil
	case "flatpak":
		if flatpak.ShouldUseDirectCache() {
			cache, err := flatpak.LoadBest()
			if err == nil && len(cache.Apps) > 0 {
				results := cache.Filter(query)
				rows := make([]searchRow, len(results))
				for i, r := range results {
					rows[i] = searchRow{Name: r.Name, Desc: r.Desc}
				}
				return rows, nil
			}
			if err == flatpak.ErrNoCache {
				_ = flatpak.UpdateAppStream()
				cache, refreshErr := flatpak.LoadBest()
				if refreshErr == nil && len(cache.Apps) > 0 {
					results := cache.Filter(query)
					rows := make([]searchRow, len(results))
					for i, r := range results {
						rows[i] = searchRow{Name: r.Name, Desc: r.Desc}
					}
					return rows, nil
				}
			}
		}
		if query == "" {
			out, err := runOutput("flatpak", "remote-ls", "--app", "--columns=application,description", "flathub")
			if err != nil {
				if errors.Is(err, context.DeadlineExceeded) {
					return nil, err
				}
				out, err = runOutput("flatpak", "remote-ls", "--app", "--columns=application,description")
				if err != nil {
					return nil, err
				}
			}
			return parseFlatpakSearch(out), nil
		}
		out, err := runOutput("flatpak", "search", "--columns=application,description", query)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return nil, err
			}
			out, err = runOutput("flatpak", "search", query)
			if err != nil {
				return nil, err
			}
		}
		return parseFlatpakSearch(out), nil
	case "npm":
		out, err := runOutput("npm", "search", query, fmt.Sprintf("--searchlimit=%d", input.NPMSearchLimit), "--parseable")
		if err != nil {
			return nil, err
		}
		return parseNpmSearch(out), nil
	case "bun":
		if !bunSearchAvailable() {
			if !input.AllowBunNPMFallback {
				return nil, nil
			}
			npmOut, npmErr := runOutput("npm", "search", query, fmt.Sprintf("--searchlimit=%d", input.NPMSearchLimit), "--parseable")
			if npmErr != nil {
				return nil, npmErr
			}
			return parseNpmSearch(npmOut), nil
		}
		out, err := runOutput("bun", "search", query)
		if err != nil {
			if _, lookupErr := exec.LookPath("npm"); lookupErr != nil || !input.AllowBunNPMFallback {
				return nil, err
			}
			npmOut, npmErr := runOutput("npm", "search", query, fmt.Sprintf("--searchlimit=%d", input.NPMSearchLimit), "--parseable")
			if npmErr != nil {
				return nil, err
			}
			return parseNpmSearch(npmOut), nil
		}
		rows := parseBunSearch(out)
		if len(rows) > 0 {
			return rows, nil
		}
		if _, lookupErr := exec.LookPath("npm"); lookupErr != nil || !input.AllowBunNPMFallback {
			return rows, nil
		}
		npmOut, npmErr := runOutput("npm", "search", query, fmt.Sprintf("--searchlimit=%d", input.NPMSearchLimit), "--parseable")
		if npmErr != nil {
			return rows, nil
		}
		return parseNpmSearch(npmOut), nil
	default:
		return nil, fmt.Errorf("unsupported manager: %s", manager)
	}
}

const defaultSearchTimeout = 30 * time.Second

func runOutputQuietErr(name string, args ...string) ([]byte, error) {
	return runOutputQuietErrWithTimeout(defaultSearchTimeout, name, args...)
}

func runOutputQuietErrWithTimeout(timeout time.Duration, name string, args ...string) ([]byte, error) {
	if name == "" {
		return nil, fmt.Errorf("empty command name")
	}
	if timeout <= 0 {
		timeout = defaultSearchTimeout
	}
	if timeout > 120*time.Second {
		timeout = 120 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = os.Environ()
	cmd.Stderr = ioDiscard{}
	out, err := cmd.Output()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return nil, context.DeadlineExceeded
	}
	return out, err
}

func runLineStreamQuietErr(name string, args []string, onLine func(string)) error {
	return runLineStreamQuietErrWithTimeout(defaultSearchTimeout, name, args, onLine)
}

func runLineStreamQuietErrWithTimeout(timeout time.Duration, name string, args []string, onLine func(string)) error {
	if name == "" {
		return fmt.Errorf("empty command name")
	}
	if onLine == nil {
		return fmt.Errorf("onLine callback is nil")
	}
	if timeout <= 0 {
		timeout = defaultSearchTimeout
	}
	if timeout > 120*time.Second {
		timeout = 120 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = os.Environ()
	cmd.Stderr = ioDiscard{}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe for %s failed: %w", name, err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %s failed: %w", name, err)
	}
	// Ensure pipe is closed when context times out or scanner finishes
	defer func() {
		_ = stdout.Close()
	}()

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1*1024*1024)
	for scanner.Scan() {
		// Recover from panics in callback to avoid crashing
		func(line string) {
			defer func() {
				if r := recover(); r != nil {
					fmt.Fprintf(os.Stderr, "fpf warning: panic in line handler for %s: %v\n", name, r)
				}
			}()
			onLine(line)
		}(scanner.Text())
	}
	scanErr := scanner.Err()
	waitErr := cmd.Wait()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	if scanErr != nil {
		return fmt.Errorf("scan %s: %w", name, scanErr)
	}
	if waitErr != nil {
		// Normalize exit errors - caller may handle differently
		return waitErr
	}
	return nil
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) {
	return len(p), nil
}

var (
	bunSearchCheckOnce sync.Once
	bunSearchReady     bool
)

func bunSearchAvailable() bool {
	bunSearchCheckOnce.Do(func() {
		if _, err := exec.LookPath("bun"); err != nil {
			bunSearchReady = false
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		cmd := exec.CommandContext(ctx, "bun", "search", "--help")
		cmd.Env = os.Environ()
		out, err := cmd.CombinedOutput()
		if err == nil {
			bunSearchReady = true
			return
		}
		text := strings.ToLower(string(out))
		if strings.Contains(text, "script not found \"search\"") || strings.Contains(text, "unknown command") {
			bunSearchReady = false
			return
		}
		// Keep behavior permissive for non-standard bun outputs.
		bunSearchReady = true
	})
	return bunSearchReady
}
