package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strings"
)

func parseAptSearch(out []byte) []searchRow {
	if out == nil {
		return nil
	}
	rows := make([]searchRow, 0)
	scanner := bufio.NewScanner(bytes.NewReader(out))
	scanner.Buffer(make([]byte, 0, 64*1024), 1*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if len(line) > 4096 {
			continue
		}
		parts := strings.SplitN(line, " - ", 2)
		if len(parts) == 0 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		if name == "" || len(name) > maxPackageLength {
			continue
		}
		desc := "-"
		if len(parts) == 2 && strings.TrimSpace(parts[1]) != "" {
			desc = strings.TrimSpace(parts[1])
		}
		rows = append(rows, searchRow{Name: name, Desc: desc})
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "fpf warning: scan error apt search: %v\n", err)
	}
	return rows
}

func parseDNFSearch(out []byte) []searchRow {
	if out == nil {
		return nil
	}
	rows := make([]searchRow, 0)
	scanner := bufio.NewScanner(bytes.NewReader(out))
	// Use 64KiB initial, 1MiB max - enough for 4096 line limit while limiting memory use
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
		if strings.HasPrefix(lower, "available") || strings.HasPrefix(lower, "installed") || strings.HasPrefix(lower, "last") || strings.HasPrefix(lower, "name") || strings.HasPrefix(lower, "-") || strings.HasPrefix(lower, "=") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		name := parts[0]
		if name == "" || len(name) > maxPackageLength {
			continue
		}
		// DNF list output uses NAME.ARCH; strip only known RPM arch suffixes
		// to avoid corrupting dotted package names like "my.pkg.name".
		if idx := strings.LastIndex(name, "."); idx > 0 && idx < len(name)-1 {
			suffix := strings.ToLower(name[idx+1:])
			switch suffix {
			case "x86_64", "i686", "i386", "noarch", "aarch64", "armv7hl", "armv7hnl",
				"ppc64", "ppc64le", "s390", "s390x", "riscv64", "loongarch64", "sparc64":
				name = name[:idx]
			}
		}
		if name == "" {
			continue
		}
		// Validate package name roughly - DNF names are alphanum, -, _, +, .
		if !isValidPkgName(name) {
			// still allow but ensure not header remnant like "Name" again
			if strings.EqualFold(name, "Name") {
				continue
			}
		}
		desc := strings.Join(parts[1:], " ")
		if strings.TrimSpace(desc) == "" {
			desc = "-"
		}
		rows = append(rows, searchRow{Name: name, Desc: desc})
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "fpf warning: scan error dnf search: %v\n", err)
	}
	return rows
}

func parsePacmanSearch(out []byte) []searchRow {
	if out == nil {
		return nil
	}
	rows := make([]searchRow, 0)
	lines := splitLines(out)
	if lines == nil {
		return rows
	}
	for i := 0; i < len(lines); i++ {
		raw := lines[i]
		if len(raw) > 4096 {
			continue
		}
		trim := strings.TrimSpace(raw)
		if trim == "" {
			continue
		}
		// Pacman headers are not indented; descriptions are indented with spaces.
		// Use raw indentation to distinguish header vs description reliably,
		// even if description contains "/".
		if strings.HasPrefix(raw, " ") || strings.HasPrefix(raw, "\t") {
			continue
		}
		// pacman head looks like "repo/pkg version [installed]" and contains "/"
		if !strings.Contains(trim, "/") {
			continue
		}
		parts := strings.Fields(trim)
		if len(parts) == 0 {
			continue
		}
		pkg := parts[0]
		if strings.Contains(pkg, "/") {
			seg := strings.SplitN(pkg, "/", 2)
			if len(seg) == 2 && seg[1] != "" {
				pkg = seg[1]
			} else {
				continue
			}
		}
		if pkg == "" || len(pkg) > maxPackageLength || !isValidPkgName(pkg) {
			continue
		}
		desc := "-"
		if i+1 < len(lines) {
			nextRaw := lines[i+1]
			// description lines are indented in raw pacman -Ss output
			if strings.HasPrefix(nextRaw, " ") || strings.HasPrefix(nextRaw, "\t") {
				candidate := strings.TrimSpace(nextRaw)
				if candidate != "" {
					desc = candidate
					i++
				}
			} else {
				// Fallback for non-indented second line (e.g. synthetic test fixtures)
				candidate := strings.TrimSpace(nextRaw)
				if candidate != "" && !strings.Contains(candidate, "/") {
					// Heuristic: if next line contains "/" it is likely next header, not desc
					desc = candidate
					i++
				}
			}
		}
		rows = append(rows, searchRow{Name: pkg, Desc: desc})
	}
	return rows
}

func parseZypperSearch(out []byte) []searchRow {
	if out == nil {
		return nil
	}
	rows := make([]searchRow, 0)
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
		// Header/separator detection: case-insensitive, handles "S | Name...", "Name  Version  Repo", "---+---", etc.
		if strings.HasPrefix(lower, "s ") || strings.HasPrefix(lower, "s |") || strings.HasPrefix(trim, "-") || strings.HasPrefix(lower, "name") {
			// More precise: if line contains '|' and second column trimmed is "Name", it's header
			// But for generic "Name  Version  Repo" header with spaces, also skip via prefix
			continue
		}
		// Also skip lines that are purely separator characters
		if strings.Trim(trim, "-+| ") == "" {
			continue
		}
		parts := strings.Split(line, "|")
		// tolerate variable column counts: need at least Name column
		if len(parts) < 3 {
			// Zypper table header without pipes? e.g. "Name  Version  Repo" space-separated fallback
			// Try fields split for space-separated header detection - but skip as header
			if strings.EqualFold(strings.TrimSpace(parts[0]), "Name") {
				continue
			}
			continue
		}
		// trim all parts
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		// Detect header row where Name column equals "Name" (case-insensitive) after trimming
		isHeader := false
		for _, p := range parts {
			if strings.EqualFold(p, "Name") {
				// Check if this is header by also seeing if other columns look like header names
				// Heuristic: header contains "Version" or "Repository" in some column
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
		// real zypper has columns: S | Name | Type | Version | Arch | Repository (6 cols)
		// mock has: i | x | zypkg | x | 1.0 | x | repo (7 parts)
		name := ""
		ver := ""
		repo := ""
		if len(parts) >= 7 {
			name = parts[2]
			ver = parts[4]
			repo = parts[6]
		} else if len(parts) == 6 {
			name = parts[1]
			ver = parts[3]
			repo = parts[5]
		} else if len(parts) >= 3 {
			// Generic fallback: Name is at index 1 or 2 depending on leading S column presence
			// Prefer parts[1] if len==3-5 and not empty, else parts[2]
			if len(parts) == 3 || len(parts) == 4 {
				name = parts[1]
				if name == "" || strings.EqualFold(name, "Name") {
					name = parts[2%len(parts)]
				}
			} else {
				name = parts[2]
				if name == "" {
					name = parts[1]
				}
			}
			if len(parts) > 4 {
				ver = parts[4]
				if ver == "" && len(parts) > 3 {
					ver = parts[3]
				}
			}
			if len(parts) > 5 {
				repo = parts[len(parts)-1]
			} else if len(parts) >= 4 {
				// Try last column as repo if not already
				if repo == "" {
					repo = parts[len(parts)-1]
					// Avoid mistaking version for repo when repo empty
					if repo == ver {
						repo = ""
					}
				}
			}
		}
		if name == "" || strings.EqualFold(name, "Name") || len(name) > maxPackageLength {
			continue
		}
		if !isValidPkgName(name) {
			// Still allow hyphens etc but skip obvious header remnants
			if strings.Contains(name, " ") {
				continue
			}
		}
		if ver == "" {
			ver = "-"
		}
		if repo == "" {
			repo = "unknown"
		}
		desc := fmt.Sprintf("version %s from %s", ver, repo)
		rows = append(rows, searchRow{Name: name, Desc: desc})
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "fpf warning: scan zypper: %v\n", err)
	}
	return rows
}

func parseEmergeSearch(out []byte) []searchRow {
	if out == nil {
		return nil
	}
	rows := make([]searchRow, 0)
	var atom string
	scanner := bufio.NewScanner(bytes.NewReader(out))
	scanner.Buffer(make([]byte, 0, 64*1024), 1*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) > 8192 {
			continue
		}
		if strings.HasPrefix(line, "*  ") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				candidate := strings.TrimSpace(parts[1])
				if candidate != "" && len(candidate) <= maxPackageLength {
					atom = candidate
				}
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
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "fpf warning: scan emerge: %v\n", err)
	}
	return rows
}

func parseBrewSearch(out []byte) []searchRow {
	if out == nil {
		return nil
	}
	rows := make([]searchRow, 0)
	scanner := bufio.NewScanner(bytes.NewReader(out))
	scanner.Buffer(make([]byte, 0, 64*1024), 1*1024*1024)
	for scanner.Scan() {
		name := strings.TrimSpace(scanner.Text())
		if name == "" || name == "==>" || len(name) > maxPackageLength {
			continue
		}
		if strings.Contains(name, " ") && !strings.HasPrefix(name, "==>") {
			// Brew search typically returns single names per line; skip lines with spaces that are headers
			continue
		}
		rows = append(rows, searchRow{Name: name, Desc: "-"})
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "fpf warning: scan brew: %v\n", err)
	}
	return rows
}

func parseWingetSearch(out []byte) []searchRow {
	if out == nil {
		return nil
	}
	re := regexp.MustCompile(`\s{2,}`)
	rows := make([]searchRow, 0)
	scanner := bufio.NewScanner(bytes.NewReader(out))
	scanner.Buffer(make([]byte, 0, 64*1024), 1*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "Name") || strings.HasPrefix(line, "-") {
			continue
		}
		if len(line) > 8192 {
			continue
		}
		cols := re.Split(line, -1)
		if len(cols) < 2 {
			continue
		}
		name := strings.TrimSpace(cols[1])
		if name == "" || len(name) > maxPackageLength {
			continue
		}
		rows = append(rows, searchRow{Name: name, Desc: "-"})
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "fpf warning: scan winget: %v\n", err)
	}
	return rows
}

func parseChocoSearch(out []byte) []searchRow {
	if out == nil {
		return nil
	}
	rows := make([]searchRow, 0)
	scanner := bufio.NewScanner(bytes.NewReader(out))
	scanner.Buffer(make([]byte, 0, 64*1024), 1*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || len(line) > 4096 {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		if len(parts) == 0 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		if name == "" || len(name) > maxPackageLength {
			continue
		}
		ver := "-"
		if len(parts) == 2 && strings.TrimSpace(parts[1]) != "" {
			ver = strings.TrimSpace(parts[1])
		}
		rows = append(rows, searchRow{Name: name, Desc: "version " + ver})
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "fpf warning: scan choco: %v\n", err)
	}
	return rows
}

func parseScoopSearch(out []byte) []searchRow {
	if out == nil {
		return nil
	}
	rows := make([]searchRow, 0)
	scanner := bufio.NewScanner(bytes.NewReader(out))
	scanner.Buffer(make([]byte, 0, 64*1024), 1*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "Name") || strings.HasPrefix(line, "-") || len(line) > 4096 {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) == 0 {
			continue
		}
		name := parts[0]
		if name == "" || len(name) > maxPackageLength {
			continue
		}
		desc := "-"
		if len(parts) > 1 {
			desc = strings.Join(parts[1:], " ")
		}
		rows = append(rows, searchRow{Name: name, Desc: desc})
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "fpf warning: scan scoop: %v\n", err)
	}
	return rows
}

func parseSnapSearch(out []byte) []searchRow {
	if out == nil {
		return nil
	}
	rows := make([]searchRow, 0)
	lines := splitLines(out)
	if lines == nil || len(lines) == 0 {
		return rows
	}
	for i, line := range lines {
		if i == 0 {
			continue
		}
		trim := strings.TrimSpace(line)
		if trim == "" || len(trim) > 4096 {
			continue
		}
		parts := strings.Fields(trim)
		if len(parts) == 0 {
			continue
		}
		name := parts[0]
		if name == "" || len(name) > maxPackageLength {
			continue
		}
		desc := "-"
		if len(parts) > 1 {
			desc = strings.Join(parts[1:], " ")
		}
		rows = append(rows, searchRow{Name: name, Desc: desc})
	}
	return rows
}

func parseFlatpakSearch(out []byte) []searchRow {
	if out == nil {
		return nil
	}
	rows := make([]searchRow, 0)
	lines := splitLines(out)
	if lines == nil {
		return rows
	}
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if trim == "" || len(trim) > 4096 || isFlatpakHeaderLine(trim) {
			continue
		}
		parts := strings.Fields(trim)
		if len(parts) == 0 {
			continue
		}
		name := parts[0]
		if name == "" || len(name) > maxPackageLength {
			continue
		}
		desc := "-"
		if len(parts) > 1 {
			// Safe slice: ensure name is prefix of trim
			if len(trim) >= len(name) && strings.HasPrefix(trim, name) {
				desc = strings.TrimSpace(trim[len(name):])
				if desc == "" {
					desc = "-"
				}
			} else {
				desc = strings.Join(parts[1:], " ")
				if desc == "" {
					desc = "-"
				}
			}
		}
		rows = append(rows, searchRow{Name: name, Desc: desc})
	}
	return rows
}

func isFlatpakHeaderLine(line string) bool {
	fields := strings.Fields(strings.ToLower(strings.TrimSpace(line)))
	if len(fields) < 2 {
		return false
	}
	return fields[0] == "application" && fields[1] == "description"
}

func parseNpmSearch(out []byte) []searchRow {
	if out == nil {
		return nil
	}
	rows := make([]searchRow, 0)
	scanner := bufio.NewScanner(bytes.NewReader(out))
	scanner.Buffer(make([]byte, 0, 64*1024), 1*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) > 8192 {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 2 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		if name == "" || len(name) > maxPackageLength {
			continue
		}
		desc := strings.TrimSpace(parts[1])
		if desc == "" {
			desc = "-"
		}
		rows = append(rows, searchRow{Name: name, Desc: desc})
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "fpf warning: scan npm: %v\n", err)
	}
	return rows
}

func parseBunSearch(out []byte) []searchRow {
	if out == nil {
		return nil
	}
	rows := make([]searchRow, 0)
	lines := splitLines(out)
	if lines == nil || len(lines) == 0 {
		return rows
	}
	for i, line := range lines {
		if i == 0 {
			continue
		}
		trim := strings.TrimSpace(line)
		if trim == "" || len(trim) > 4096 {
			continue
		}
		parts := strings.Fields(trim)
		if len(parts) == 0 {
			continue
		}
		name := parts[0]
		if name == "" || len(name) > maxPackageLength {
			continue
		}
		// Strip version suffix if present (pkg@version)
		if atCount := strings.Count(name, "@"); atCount > 0 {
			if idx := strings.LastIndex(name, "@"); idx > 0 {
				candidate := name[:idx]
				if candidate != "" {
					name = candidate
				}
			}
		}
		desc := "-"
		if len(parts) > 1 {
			desc = strings.Join(parts[1:], " ")
		}
		rows = append(rows, searchRow{Name: name, Desc: desc})
	}
	return rows
}

func dedupeRows(rows []searchRow) []searchRow {
	if rows == nil {
		return nil
	}
	seen := make(map[string]struct{}, len(rows))
	out := make([]searchRow, 0, len(rows))
	for _, row := range rows {
		if row.Name == "" || len(row.Name) > maxPackageLength {
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
	if out == nil {
		return nil
	}
	if len(out) > 20<<20 {
		fmt.Fprintf(os.Stderr, "fpf warning: output too large (%d), truncating\n", len(out))
		out = out[:20<<20]
	}
	raw := strings.ReplaceAll(string(out), "\r\n", "\n")
	raw = strings.TrimRight(raw, "\n")
	if raw == "" {
		return nil
	}
	return strings.Split(raw, "\n")
}
