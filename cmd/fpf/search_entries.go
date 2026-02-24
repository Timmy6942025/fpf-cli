package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

type searchInput struct {
	Manager        string
	Query          string
	Limit          int
	NPMSearchLimit int
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
		return true, 0
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
	input := searchInput{NPMSearchLimit: 500}
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
			fmt.Sscanf(args[i+1], "%d", &input.Limit)
			i++
		case "--go-npm-search-limit":
			if !modeEnabled {
				continue
			}
			hasFlag = true
			if i+1 >= len(args) {
				return input, true, errors.New("missing value for --go-npm-search-limit")
			}
			fmt.Sscanf(args[i+1], "%d", &input.NPMSearchLimit)
			i++
		}
	}

	if !modeEnabled || !hasFlag {
		return input, false, nil
	}
	if input.Manager == "" {
		return input, true, errors.New("--go-manager is required")
	}
	if input.NPMSearchLimit <= 0 {
		input.NPMSearchLimit = 500
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

	switch manager {
	case "apt":
		out, err := runOutputQuietErr("apt-cache", "search", "--", query)
		if err != nil {
			return nil, err
		}
		return parseAptSearch(out), nil
	case "dnf":
		pattern := "*"
		if query != "" {
			pattern = "*" + query + "*"
		}
		out, err := runOutputQuietErr("dnf", "-q", "list", "available", pattern)
		if err != nil {
			return nil, err
		}
		return parseDNFSearch(out), nil
	case "pacman":
		out, err := runOutputQuietErr("pacman", "-Ss", "--", query)
		if err != nil {
			return nil, err
		}
		return parsePacmanSearch(out), nil
	case "zypper":
		out, err := runOutputQuietErr("zypper", "--non-interactive", "--quiet", "search", "--details", "--type", "package", query)
		if err != nil {
			return nil, err
		}
		return parseZypperSearch(out), nil
	case "emerge":
		out, err := runOutputQuietErr("emerge", "--searchdesc", "--color=n", query)
		if err != nil {
			return nil, err
		}
		return parseEmergeSearch(out), nil
	case "brew":
		out, err := runOutputQuietErr("brew", "search", query)
		if err != nil {
			return nil, err
		}
		return parseBrewSearch(out), nil
	case "winget":
		out, err := runOutputQuietErr("winget", "search", query, "--source", "winget", "--accept-source-agreements", "--disable-interactivity")
		if err != nil {
			return nil, err
		}
		return parseWingetSearch(out), nil
	case "choco":
		out, err := runOutputQuietErr("choco", "search", query, "--limit-output")
		if err != nil {
			return nil, err
		}
		return parseChocoSearch(out), nil
	case "scoop":
		out, err := runOutputQuietErr("scoop", "search", query)
		if err != nil {
			return nil, err
		}
		return parseScoopSearch(out), nil
	case "snap":
		out, err := runOutputQuietErr("snap", "find", query)
		if err != nil {
			return nil, err
		}
		return parseSnapSearch(out), nil
	case "flatpak":
		if query == "" {
			out, err := runOutputQuietErr("flatpak", "remote-ls", "--app", "--columns=application,description", "flathub")
			if err != nil {
				out, err = runOutputQuietErr("flatpak", "remote-ls", "--app", "--columns=application,description")
				if err != nil {
					return nil, err
				}
			}
			return parseFlatpakSearch(out), nil
		}
		out, err := runOutputQuietErr("flatpak", "search", "--columns=application,description", query)
		if err != nil {
			out, err = runOutputQuietErr("flatpak", "search", query)
			if err != nil {
				return nil, err
			}
		}
		return parseFlatpakSearch(out), nil
	case "npm":
		out, err := runOutputQuietErr("npm", "search", query, fmt.Sprintf("--searchlimit=%d", input.NPMSearchLimit), "--parseable")
		if err != nil {
			return nil, err
		}
		return parseNpmSearch(out), nil
	case "bun":
		out, err := runOutputQuietErr("bun", "search", query)
		if err != nil {
			if _, lookupErr := exec.LookPath("npm"); lookupErr != nil {
				return nil, err
			}
			npmOut, npmErr := runOutputQuietErr("npm", "search", query, fmt.Sprintf("--searchlimit=%d", input.NPMSearchLimit), "--parseable")
			if npmErr != nil {
				return nil, err
			}
			return parseNpmSearch(npmOut), nil
		}
		return parseBunSearch(out), nil
	default:
		return nil, fmt.Errorf("unsupported manager: %s", manager)
	}
}

func runOutputQuietErr(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	cmd.Env = os.Environ()
	cmd.Stderr = ioDiscard{}
	return cmd.Output()
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) {
	return len(p), nil
}

func parseAptSearch(out []byte) []searchRow {
	rows := make([]searchRow, 0)
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, " - ", 2)
		name := strings.TrimSpace(parts[0])
		desc := "-"
		if len(parts) == 2 && strings.TrimSpace(parts[1]) != "" {
			desc = strings.TrimSpace(parts[1])
		}
		rows = append(rows, searchRow{Name: name, Desc: desc})
	}
	return rows
}

func parseDNFSearch(out []byte) []searchRow {
	rows := make([]searchRow, 0)
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "Available") || strings.HasPrefix(line, "Last") || strings.HasPrefix(line, "Installed") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		name := parts[0]
		if idx := strings.LastIndex(name, "."); idx > 0 {
			name = name[:idx]
		}
		rows = append(rows, searchRow{Name: name, Desc: parts[1]})
	}
	return rows
}

func parsePacmanSearch(out []byte) []searchRow {
	rows := make([]searchRow, 0)
	lines := splitLines(out)
	for i := 0; i+1 < len(lines); i += 2 {
		head := strings.TrimSpace(lines[i])
		if head == "" {
			continue
		}
		parts := strings.Fields(head)
		if len(parts) == 0 {
			continue
		}
		pkg := parts[0]
		if strings.Contains(pkg, "/") {
			seg := strings.SplitN(pkg, "/", 2)
			if len(seg) == 2 {
				pkg = seg[1]
			}
		}
		desc := strings.TrimSpace(lines[i+1])
		if desc == "" {
			desc = "-"
		}
		rows = append(rows, searchRow{Name: pkg, Desc: desc})
	}
	return rows
}

func parseZypperSearch(out []byte) []searchRow {
	rows := make([]searchRow, 0)
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Split(line, "|")
		if len(parts) < 7 {
			continue
		}
		name := strings.TrimSpace(parts[2])
		ver := strings.TrimSpace(parts[4])
		repo := strings.TrimSpace(parts[6])
		if name == "" {
			continue
		}
		desc := fmt.Sprintf("version %s from %s", ver, repo)
		rows = append(rows, searchRow{Name: name, Desc: desc})
	}
	return rows
}

func parseEmergeSearch(out []byte) []searchRow {
	rows := make([]searchRow, 0)
	var atom string
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "*  ") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				atom = parts[1]
			}
			continue
		}
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "Description:") && atom != "" {
			desc := strings.TrimSpace(strings.TrimPrefix(trim, "Description:"))
			if desc == "" {
				desc = "-"
			}
			rows = append(rows, searchRow{Name: atom, Desc: desc})
			atom = ""
		}
	}
	return rows
}

func parseBrewSearch(out []byte) []searchRow {
	rows := make([]searchRow, 0)
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		name := strings.TrimSpace(scanner.Text())
		if name == "" || name == "==>" {
			continue
		}
		rows = append(rows, searchRow{Name: name, Desc: "-"})
	}
	return rows
}

func parseWingetSearch(out []byte) []searchRow {
	re := regexp.MustCompile(`\s{2,}`)
	rows := make([]searchRow, 0)
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "Name") || strings.HasPrefix(line, "-") {
			continue
		}
		cols := re.Split(line, -1)
		if len(cols) < 2 {
			continue
		}
		rows = append(rows, searchRow{Name: cols[1], Desc: "-"})
	}
	return rows
}

func parseChocoSearch(out []byte) []searchRow {
	rows := make([]searchRow, 0)
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		name := strings.TrimSpace(parts[0])
		if name == "" {
			continue
		}
		ver := "-"
		if len(parts) == 2 && strings.TrimSpace(parts[1]) != "" {
			ver = strings.TrimSpace(parts[1])
		}
		rows = append(rows, searchRow{Name: name, Desc: "version " + ver})
	}
	return rows
}

func parseScoopSearch(out []byte) []searchRow {
	rows := make([]searchRow, 0)
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "Name") || strings.HasPrefix(line, "-") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) == 0 {
			continue
		}
		name := parts[0]
		desc := "-"
		if len(parts) > 1 {
			desc = strings.Join(parts[1:], " ")
		}
		rows = append(rows, searchRow{Name: name, Desc: desc})
	}
	return rows
}

func parseSnapSearch(out []byte) []searchRow {
	rows := make([]searchRow, 0)
	lines := splitLines(out)
	for i, line := range lines {
		trim := strings.TrimSpace(line)
		if i == 0 || trim == "" {
			continue
		}
		parts := strings.Fields(trim)
		if len(parts) == 0 {
			continue
		}
		name := parts[0]
		desc := "-"
		if len(parts) > 1 {
			desc = strings.Join(parts[1:], " ")
		}
		rows = append(rows, searchRow{Name: name, Desc: desc})
	}
	return rows
}

func parseFlatpakSearch(out []byte) []searchRow {
	rows := make([]searchRow, 0)
	lines := splitLines(out)
	for i, line := range lines {
		trim := strings.TrimSpace(line)
		if i == 0 || trim == "" {
			continue
		}
		parts := strings.Fields(trim)
		if len(parts) == 0 {
			continue
		}
		name := parts[0]
		desc := "-"
		if len(parts) > 1 {
			desc = strings.TrimSpace(trim[len(name):])
			if desc == "" {
				desc = "-"
			}
		}
		rows = append(rows, searchRow{Name: name, Desc: desc})
	}
	return rows
}

func parseNpmSearch(out []byte) []searchRow {
	rows := make([]searchRow, 0)
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Split(line, "\t")
		if len(parts) < 2 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		desc := strings.TrimSpace(parts[1])
		if name == "" {
			continue
		}
		if desc == "" {
			desc = "-"
		}
		rows = append(rows, searchRow{Name: name, Desc: desc})
	}
	return rows
}

func parseBunSearch(out []byte) []searchRow {
	rows := make([]searchRow, 0)
	lines := splitLines(out)
	for i, line := range lines {
		if i == 0 {
			continue
		}
		trim := strings.TrimSpace(line)
		if trim == "" {
			continue
		}
		parts := strings.Fields(trim)
		if len(parts) == 0 {
			continue
		}
		name := parts[0]
		desc := "-"
		if len(parts) > 1 {
			desc = strings.Join(parts[1:], " ")
		}
		rows = append(rows, searchRow{Name: name, Desc: desc})
	}
	return rows
}

func dedupeRows(rows []searchRow) []searchRow {
	seen := make(map[string]struct{}, len(rows))
	out := make([]searchRow, 0, len(rows))
	for _, row := range rows {
		if row.Name == "" {
			continue
		}
		if _, ok := seen[row.Name]; ok {
			continue
		}
		seen[row.Name] = struct{}{}
		if row.Desc == "" {
			row.Desc = "-"
		}
		out = append(out, row)
	}
	return out
}

func splitLines(out []byte) []string {
	raw := strings.ReplaceAll(string(out), "\r\n", "\n")
	raw = strings.TrimRight(raw, "\n")
	if raw == "" {
		return nil
	}
	return strings.Split(raw, "\n")
}
