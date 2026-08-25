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
		fmt.Fprintf(os.Stderr, "fpf-go: installed lookup failed for %s: %v\n", input.Manager, err)
		return true, 1
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
	if err := validateManagerName(input.Manager); err != nil {
		return input, true, err
	}

	return input, true, nil
}

func executeInstalledEntries(input installedInput) ([]string, error) {
	manager := input.Manager
	if err := validateManagerName(manager); err != nil {
		return nil, err
	}

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
	if out == nil {
		return nil
	}
	names := make([]string, 0)
	scanner := bufio.NewScanner(bytes.NewReader(out))
	scanner.Buffer(make([]byte, 0, 64*1024), 1*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) > 0 && parts[0] != "" {
			candidate := strings.TrimSpace(parts[0])
			if candidate != "" && len(candidate) <= maxPackageLength {
				names = append(names, candidate)
			}
		}
	}
	return names
}

func parseBrewInstalled(out []byte) []string {
	if out == nil {
		return nil
	}
	names := make([]string, 0)
	scanner := bufio.NewScanner(bytes.NewReader(out))
	scanner.Buffer(make([]byte, 0, 64*1024), 1*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) > 0 {
			candidate := strings.TrimSpace(parts[0])
			if candidate != "" && len(candidate) <= maxPackageLength && isValidPkgName(candidate) {
				names = append(names, candidate)
			}
		}
	}
	return names
}

func parseDnfInstalled(out []byte) []string {
	if out == nil {
		return nil
	}
	names := make([]string, 0)
	scanner := bufio.NewScanner(bytes.NewReader(out))
	scanner.Buffer(make([]byte, 0, 64*1024), 1*1024*1024)
	for scanner.Scan() {
		raw := scanner.Text()
		if len(raw) > 4096 {
			continue
		}
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "installed") || strings.HasPrefix(lower, "last") || strings.HasPrefix(lower, "available") || strings.HasPrefix(lower, "name") || strings.HasPrefix(lower, "-") || strings.HasPrefix(lower, "=") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) == 0 {
			continue
		}
		name := parts[0]
		if idx := strings.LastIndex(name, "."); idx > 0 && idx < len(name)-1 {
			suffix := strings.ToLower(name[idx+1:])
			switch suffix {
			case "x86_64", "i686", "i386", "noarch", "aarch64", "armv7hl", "armv7hnl",
				"ppc64", "ppc64le", "s390", "s390x", "riscv64", "loongarch64", "sparc64":
				name = name[:idx]
			}
		}
		if name != "" && len(name) <= maxPackageLength && name != "" {
			// Skip header remnants
			if strings.EqualFold(name, "Name") {
				continue
			}
			names = append(names, name)
		}
	}
	return names
}

func parsePacmanInstalled(out []byte) []string {
	if out == nil {
		return nil
	}
	names := make([]string, 0)
	scanner := bufio.NewScanner(bytes.NewReader(out))
	scanner.Buffer(make([]byte, 0, 64*1024), 1*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) > 0 {
			candidate := strings.TrimSpace(parts[0])
			if candidate != "" && len(candidate) <= maxPackageLength && isValidPkgName(candidate) {
				names = append(names, candidate)
			}
		}
	}
	return names
}

func parseZypperInstalled(out []byte) []string {
	if out == nil {
		return nil
	}
	names := make([]string, 0)
	scanner := bufio.NewScanner(bytes.NewReader(out))
	scanner.Buffer(make([]byte, 0, 64*1024), 1*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) > 8192 {
			continue
		}
		trim := strings.TrimSpace(line)
		if trim == "" {
			continue
		}
		lower := strings.ToLower(trim)
		if strings.HasPrefix(lower, "s ") || strings.HasPrefix(lower, "s |") || strings.HasPrefix(trim, "-") || strings.HasPrefix(lower, "name") {
			continue
		}
		if strings.Trim(trim, "-+| ") == "" {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) < 3 {
			continue
		}
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		// header detection
		isHeader := false
		for _, p := range parts {
			if strings.EqualFold(p, "Name") {
				for _, q := range parts {
					if strings.EqualFold(q, "Version") || strings.EqualFold(q, "Repository") || strings.EqualFold(q, "Type") {
						isHeader = true
						break
					}
				}
				if isHeader {
					break
				}
			}
		}
		if isHeader {
			continue
		}
		name := ""
		if len(parts) >= 7 {
			name = parts[2]
		} else if len(parts) >= 6 {
			// real zypper: S | Name | Type | Version | Arch | Repository
			name = parts[1]
		} else if len(parts) >= 3 {
			name = parts[2]
			if name == "" || strings.EqualFold(name, "Name") {
				if len(parts) > 1 {
					// fallback for malformed lines
					alt := parts[1]
					if alt != "" && !strings.EqualFold(alt, "Name") {
						name = alt
					}
				}
			}
		}
		if name == "" || strings.EqualFold(name, "Name") || len(name) > maxPackageLength {
			continue
		}
		names = append(names, name)
	}
	return names
}

func parseEmergeInstalled(out []byte) []string {
	if out == nil {
		return nil
	}
	names := make([]string, 0)
	scanner := bufio.NewScanner(bytes.NewReader(out))
	scanner.Buffer(make([]byte, 0, 64*1024), 1*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) > 0 {
			candidate := strings.TrimSpace(parts[0])
			if candidate != "" && len(candidate) <= maxPackageLength && isValidPkgName(candidate) {
				names = append(names, candidate)
			}
		}
	}
	return names
}

func parseWingetInstalled(out []byte) []string {
	if out == nil {
		return nil
	}
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
			candidate := strings.TrimSpace(cols[1])
			if candidate != "" && len(candidate) <= maxPackageLength {
				names = append(names, candidate)
			}
		}
	}
	return names
}

func parseChocoInstalled(out []byte) []string {
	if out == nil {
		return nil
	}
	names := make([]string, 0)
	scanner := bufio.NewScanner(bytes.NewReader(out))
	scanner.Buffer(make([]byte, 0, 64*1024), 1*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		if len(parts) > 0 && parts[0] != "" {
			candidate := strings.TrimSpace(parts[0])
			if candidate != "" && len(candidate) <= maxPackageLength && isValidPkgName(candidate) {
				names = append(names, candidate)
			}
		}
	}
	return names
}

func parseScoopInstalled(out []byte) []string {
	if out == nil {
		return nil
	}
	names := make([]string, 0)
	scanner := bufio.NewScanner(bytes.NewReader(out))
	scanner.Buffer(make([]byte, 0, 64*1024), 1*1024*1024)
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
			candidate := strings.TrimSpace(parts[0])
			if candidate != "" && len(candidate) <= maxPackageLength && isValidPkgName(candidate) {
				names = append(names, candidate)
			}
		}
	}
	return names
}

func parseSnapInstalled(out []byte) []string {
	if out == nil {
		return nil
	}
	names := make([]string, 0)
	lines := splitLines(out)
	for i, line := range lines {
		if i == 0 || strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) > 0 {
			candidate := strings.TrimSpace(parts[0])
			if candidate != "" && len(candidate) <= maxPackageLength && isValidPkgName(candidate) {
				names = append(names, candidate)
			}
		}
	}
	return names
}

func parseFlatpakInstalled(out []byte) []string {
	if out == nil {
		return nil
	}
	names := make([]string, 0)
	lines := splitLines(out)
	for i, line := range lines {
		if i == 0 || strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) > 0 {
			candidate := strings.TrimSpace(parts[0])
			if candidate != "" && len(candidate) <= maxPackageLength && isValidPkgName(candidate) {
				names = append(names, candidate)
			}
		}
	}
	return names
}

func parseNpmInstalled(out []byte) []string {
	if out == nil {
		return nil
	}
	names := make([]string, 0)
	scanner := bufio.NewScanner(bytes.NewReader(out))
	scanner.Buffer(make([]byte, 0, 64*1024), 1*1024*1024)
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
	if out == nil {
		return nil
	}
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
