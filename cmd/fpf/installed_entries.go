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

type installedInput struct {
	Manager string
}

func maybeRunGoInstalledEntries(args []string) (bool, int) {
	input, ok, err := parseInstalledInput(args)
	if !ok {
		return false, 0
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "fpf-go: %v\n", err)
		return true, 2
	}

	names, err := executeInstalledEntries(input)
	if err != nil {
		return true, 0
	}

	for _, name := range names {
		fmt.Println(name)
	}

	return true, 0
}

func parseInstalledInput(args []string) (installedInput, bool, error) {
	input := installedInput{}
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
		case "--go-installed-entries":
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
		}
	}

	if !modeEnabled || !hasFlag {
		return input, false, nil
	}
	if input.Manager == "" {
		return input, true, errors.New("--go-manager is required")
	}

	return input, true, nil
}

func executeInstalledEntries(input installedInput) ([]string, error) {
	manager := input.Manager

	switch manager {
	case "apt":
		out, err := runOutputQuietErr("dpkg-query", "-W", "-f=${binary:Package}\\t${Version}\\n")
		if err != nil {
			return nil, err
		}
		return parseAptInstalled(out), nil
	case "brew":
		out, err := runOutputQuietErr("brew", "list", "--versions")
		if err != nil {
			return nil, err
		}
		return parseBrewInstalled(out), nil
	case "dnf":
		out, err := runOutputQuietErr("dnf", "-q", "list", "installed")
		if err != nil {
			return nil, err
		}
		return parseDnfInstalled(out), nil
	case "pacman":
		out, err := runOutputQuietErr("pacman", "-Q")
		if err != nil {
			return nil, err
		}
		return parsePacmanInstalled(out), nil
	case "zypper":
		out, err := runOutputQuietErr("zypper", "--non-interactive", "--quiet", "search", "--installed-only", "--details", "--type", "package")
		if err != nil {
			return nil, err
		}
		return parseZypperInstalled(out), nil
	case "emerge":
		out, err := runOutputQuietErr("qlist", "-ICv")
		if err != nil {
			return nil, err
		}
		return parseEmergeInstalled(out), nil
	case "winget":
		out, err := runOutputQuietErr("winget", "list", "--source", "winget", "--accept-source-agreements", "--disable-interactivity")
		if err != nil {
			return nil, err
		}
		return parseWingetInstalled(out), nil
	case "choco":
		out, err := runOutputQuietErr("choco", "list", "--local-only", "--limit-output")
		if err != nil {
			return nil, err
		}
		return parseChocoInstalled(out), nil
	case "scoop":
		out, err := runOutputQuietErr("scoop", "list")
		if err != nil {
			return nil, err
		}
		return parseScoopInstalled(out), nil
	case "snap":
		out, err := runOutputQuietErr("snap", "list")
		if err != nil {
			return nil, err
		}
		return parseSnapInstalled(out), nil
	case "flatpak":
		out, err := runOutputQuietErr("flatpak", "list", "--app", "--columns=application,version")
		if err != nil {
			return nil, err
		}
		return parseFlatpakInstalled(out), nil
	case "npm":
		out, err := runOutputQuietErr("npm", "ls", "-g", "--depth=0", "--parseable")
		if err != nil {
			return nil, err
		}
		return parseNpmInstalled(out), nil
	case "bun":
		out, err := runOutputQuietErr("bun", "pm", "ls", "--global")
		if err != nil {
			if _, lookupErr := exec.LookPath("npm"); lookupErr == nil {
				npmOut, npmErr := runOutputQuietErr("npm", "ls", "-g", "--depth=0", "--parseable")
				if npmErr == nil {
					return parseNpmInstalled(npmOut), nil
				}
			}
			return nil, err
		}
		return parseBunInstalled(out), nil
	default:
		return nil, fmt.Errorf("unsupported manager: %s", manager)
	}
}

func parseAptInstalled(out []byte) []string {
	names := make([]string, 0)
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) > 0 && parts[0] != "" {
			names = append(names, parts[0])
		}
	}
	return names
}

func parseBrewInstalled(out []byte) []string {
	names := make([]string, 0)
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) > 0 {
			names = append(names, parts[0])
		}
	}
	return names
}

func parseDnfInstalled(out []byte) []string {
	names := make([]string, 0)
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "Installed") || strings.HasPrefix(line, "Last") || strings.HasPrefix(line, "Available") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) > 0 {
			name := parts[0]
			if idx := strings.LastIndex(name, "."); idx > 0 {
				name = name[:idx]
			}
			names = append(names, name)
		}
	}
	return names
}

func parsePacmanInstalled(out []byte) []string {
	names := make([]string, 0)
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) > 0 {
			names = append(names, parts[0])
		}
	}
	return names
}

func parseZypperInstalled(out []byte) []string {
	names := make([]string, 0)
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Split(line, "|")
		if len(parts) < 3 {
			continue
		}
		name := strings.TrimSpace(parts[2])
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

func parseEmergeInstalled(out []byte) []string {
	names := make([]string, 0)
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) > 0 {
			names = append(names, parts[0])
		}
	}
	return names
}

func parseWingetInstalled(out []byte) []string {
	re := regexp.MustCompile(`\s{2,}`)
	names := make([]string, 0)
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "Name") || strings.HasPrefix(line, "-") {
			continue
		}
		cols := re.Split(line, -1)
		if len(cols) >= 2 {
			names = append(names, cols[1])
		}
	}
	return names
}

func parseChocoInstalled(out []byte) []string {
	names := make([]string, 0)
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		if len(parts) > 0 && parts[0] != "" {
			names = append(names, strings.TrimSpace(parts[0]))
		}
	}
	return names
}

func parseScoopInstalled(out []byte) []string {
	names := make([]string, 0)
	scanner := bufio.NewScanner(bytes.NewReader(out))
	inPackages := false
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "Name") || strings.HasPrefix(line, "---") {
			inPackages = true
			continue
		}
		if !inPackages {
			continue
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) > 0 {
			names = append(names, parts[0])
		}
	}
	return names
}

func parseSnapInstalled(out []byte) []string {
	names := make([]string, 0)
	lines := splitLines(out)
	for i, line := range lines {
		if i == 0 || strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) > 0 {
			names = append(names, parts[0])
		}
	}
	return names
}

func parseFlatpakInstalled(out []byte) []string {
	names := make([]string, 0)
	lines := splitLines(out)
	for i, line := range lines {
		if i == 0 || strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) > 0 {
			names = append(names, parts[0])
		}
	}
	return names
}

func parseNpmInstalled(out []byte) []string {
	names := make([]string, 0)
	scanner := bufio.NewScanner(bytes.NewReader(out))
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		if lineNumber == 1 {
			continue
		}

		line := scanner.Text()
		line = strings.ReplaceAll(line, "\r", "")
		line = strings.ReplaceAll(line, "\\", "/")

		parts := strings.Split(line, "/")
		if len(parts) == 0 {
			continue
		}

		pkg := ""
		for i := len(parts) - 1; i >= 0; i-- {
			if strings.TrimSpace(parts[i]) != "" {
				pkg = strings.TrimSpace(parts[i])
				break
			}
		}
		if pkg == "" {
			continue
		}

		for i := len(parts) - 2; i >= 0; i-- {
			prev := strings.TrimSpace(parts[i])
			if prev == "" {
				continue
			}
			if strings.HasPrefix(prev, "@") {
				pkg = prev + "/" + pkg
			}
			break
		}

		names = append(names, pkg)
	}
	return names
}

func parseBunInstalled(out []byte) []string {
	names := make([]string, 0)
	lines := splitLines(out)
	for i, rawLine := range lines {
		if i == 0 {
			continue
		}

		line := strings.ReplaceAll(rawLine, "\r", "")
		line = strings.TrimSpace(line)
		line = strings.TrimLeft(line, "+-|` ")
		line = strings.TrimSpace(line)

		if line == "" {
			continue
		}
		if strings.Contains(line, "node_modules") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}

		pkg := fields[0]

		atCount := strings.Count(pkg, "@")
		if (strings.HasPrefix(pkg, "@") && atCount >= 2) || (!strings.HasPrefix(pkg, "@") && atCount >= 1) {
			idx := strings.LastIndex(pkg, "@")
			if idx > 0 {
				pkg = pkg[:idx]
			}
		}

		if pkg != "" {
			names = append(names, pkg)
		}
	}
	return names
}
