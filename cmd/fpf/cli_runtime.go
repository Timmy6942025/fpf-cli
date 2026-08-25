package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type cliAction string

const (
	actionSearch  cliAction = "search"
	actionList    cliAction = "list"
	actionRemove  cliAction = "remove"
	actionUpdate  cliAction = "update"
	actionRefresh cliAction = "refresh"
	actionHelp    cliAction = "help"
	actionVersion cliAction = "version"
	actionFeed    cliAction = "feed-search"
)

const fzfMinVersionChangeReload = "0.56.1"

type cliInput struct {
	Action          cliAction
	AssumeYes       bool
	ManagerOverride string
	QueryParts      []string
}

type displayRow struct {
	Manager string
	Package string
	Desc    string
}

func runCLI(args []string) int {
	input, err := parseCLIInput(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		return 1
	}

	if input.Action == actionHelp {
		printCLIHelp()
		return 0
	}
	if input.Action == actionVersion {
		version, vErr := resolvePackageVersion()
		if vErr != nil {
			fmt.Fprintf(os.Stderr, "fpf-go: %v\n", vErr)
			return 1
		}
		fmt.Printf("fpf %s\n", version)
		return 0
	}

	query := strings.TrimSpace(strings.Join(input.QueryParts, " "))
	if len(query) > maxQueryLength {
		fmt.Fprintf(os.Stderr, "fpf warning: query too long (%d), truncating to %d\n", len(query), maxQueryLength)
		query = query[:maxQueryLength]
	}
	if strings.Contains(query, "\x00") {
		fmt.Fprintln(os.Stderr, "Invalid query: contains null byte")
		return 1
	}
	managers := resolveManagers(input.ManagerOverride, input.Action, query)
	if len(managers) == 0 {
		fmt.Fprintln(os.Stderr, "Unable to auto-detect supported package managers. Use --manager.")
		return 1
	}
	if len(managers) > 20 {
		fmt.Fprintln(os.Stderr, "Too many managers detected, limiting to 20")
		managers = managers[:20]
	}
	managerDisplay := joinManagerLabelsGo(managers)

	if input.Action == actionUpdate {
		if !confirmActionGo(input.AssumeYes, "Run update/upgrade for "+managerDisplay+"?") {
			fmt.Fprintln(os.Stderr, "Update canceled")
			return 0
		}
		for _, manager := range managers {
			fmt.Fprintf(os.Stderr, "Updating with %s\n", managerLabelGo(manager))
			if err := executeManagerAction(managerActionInput{Action: "update", Manager: manager}); err != nil {
				fmt.Fprintf(os.Stderr, "fpf-go: %v\n", err)
				return 1
			}
		}
		return 0
	}

	if input.Action == actionRefresh {
		if !confirmActionGo(input.AssumeYes, "Refresh package catalogs for "+managerDisplay+"?") {
			fmt.Fprintln(os.Stderr, "Refresh canceled")
			return 0
		}
		for _, manager := range managers {
			fmt.Fprintf(os.Stderr, "Refreshing catalogs with %s\n", managerLabelGo(manager))
			if err := executeManagerAction(managerActionInput{Action: "refresh", Manager: manager}); err != nil {
				fmt.Fprintf(os.Stderr, "fpf-go: %v\n", err)
				return 1
			}
		}
		return 0
	}

	displayRows := make([]displayRow, 0)
	if input.Action == actionSearch || input.Action == actionFeed {
		displayRows = collectSearchDisplayRowsGo(query, managers)
	} else {
		displayRows = collectInstalledDisplayRowsGo(managers)
	}

	if len(displayRows) == 0 {
		if input.Action == actionSearch && query == "" && len(managers) == 1 {
			if message := managerNoQuerySetupMessageGo(managers[0]); message != "" {
				fmt.Fprintln(os.Stderr, message)
				return 1
			}
		}
		if query != "" {
			fmt.Fprintf(os.Stderr, "No packages found for %s matching '%s'. Try a broader query or --manager.\n", managerDisplay, query)
		} else {
			fmt.Fprintf(os.Stderr, "No packages found for %s. Try adding a query or using --manager.\n", managerDisplay)
		}
		return 1
	}

	if input.Action == actionFeed {
		for _, row := range displayRows {
			fmt.Printf("%s\t%s\t%s\n", row.Manager, row.Package, row.Desc)
		}
		return 0
	}

	if err := ensureFzfGo(managers); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}

	sessionOverride := strings.TrimSpace(os.Getenv("FPF_SESSION_TMP_ROOT"))
	if sessionOverride != "" && !isSafeSessionPath(sessionOverride) {
		fmt.Fprintf(os.Stderr, "fpf warning: invalid FPF_SESSION_TMP_ROOT %q, falling back to %q\n", sessionOverride, filepath.Join(os.TempDir(), "fpf"))
		sessionOverride = filepath.Join(os.TempDir(), "fpf")
	}
	tmpDir := ""
	if sessionOverride != "" {
		if info, err := os.Lstat(sessionOverride); err == nil && info.Mode()&os.ModeSymlink != 0 {
			fmt.Fprintf(os.Stderr, "fpf-go: session path %q is a symlink, refusing to use\n", sessionOverride)
			return 1
		}
		if err := os.MkdirAll(sessionOverride, 0o700); err != nil {
			fmt.Fprintf(os.Stderr, "fpf-go: %v\n", err)
			return 1
		}
		if info, err := os.Lstat(sessionOverride); err == nil && info.Mode()&os.ModeSymlink != 0 {
			fmt.Fprintf(os.Stderr, "fpf-go: session path %q is a symlink after creation, refusing to use\n", sessionOverride)
			return 1
		}
		tmpDir = sessionOverride
	} else {
		tmpRoot := os.TempDir()
		if tmpRoot == "" {
			tmpRoot = "/tmp"
		}
		if info, err := os.Lstat(tmpRoot); err == nil && info.Mode()&os.ModeSymlink != 0 {
			fmt.Fprintf(os.Stderr, "fpf-go: TempDir %q is symlink, using /tmp\n", tmpRoot)
			tmpRoot = "/tmp"
		}
		sessionBase := filepath.Join(tmpRoot, "fpf")
		if info, err := os.Lstat(sessionBase); err == nil && info.Mode()&os.ModeSymlink != 0 {
			fmt.Fprintf(os.Stderr, "fpf-go: session base %q is a symlink, refusing to use\n", sessionBase)
			return 1
		}
		if mkErr := os.MkdirAll(sessionBase, 0o700); mkErr != nil {
			fmt.Fprintf(os.Stderr, "fpf-go: %v\n", mkErr)
			return 1
		}
		if info, err := os.Lstat(sessionBase); err == nil && info.Mode()&os.ModeSymlink != 0 {
			fmt.Fprintf(os.Stderr, "fpf-go: session base %q is a symlink after creation, refusing to use\n", sessionBase)
			return 1
		}
		created, err := os.MkdirTemp(sessionBase, "session.")
		if err != nil {
			fmt.Fprintf(os.Stderr, "fpf-go: failed to create session temp dir: %v\n", err)
			return 1
		}
		if created == "" || !isSafeSessionPath(created) {
			fmt.Fprintf(os.Stderr, "fpf-go: created session path %q is unsafe\n", created)
			_ = os.RemoveAll(created)
			return 1
		}
		tmpDir = created
		defer os.RemoveAll(tmpDir)
	}

	displayFile := filepath.Join(tmpDir, "display.tsv")
	if err := writeDisplayRows(displayFile, displayRows); err != nil {
		fmt.Fprintf(os.Stderr, "fpf-go: %v\n", err)
		return 1
	}

	header := "Select package(s)"
	switch input.Action {
	case actionSearch:
		header = "Select package(s) to install with " + managerDisplay + " (TAB to multi-select, * = installed)"
	case actionList:
		header = "Select installed package(s) to inspect from " + managerDisplay
	case actionRemove:
		header = "Select installed package(s) to remove from " + managerDisplay
	}

	helpFile := filepath.Join(tmpDir, "help")
	keybindFile := filepath.Join(tmpDir, "keybinds")
	if err := os.WriteFile(helpFile, []byte(buildHelpTextGo(managers)), 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "fpf-go: failed to write help file: %v\n", err)
		return 1
	}
	if err := os.WriteFile(keybindFile, []byte(buildKeybindTextGo()), 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "fpf-go: failed to write keybind file: %v\n", err)
		return 1
	}

	reloadCmd := ""
	reloadFullCmd := ""
	reloadIPCCmd := ""
	reloadFallbackFile := displayFile
	baselineFile := ""
	if input.Action == actionSearch && dynamicReloadEnabledGo(len(managers)) {
		if strings.TrimSpace(query) != "" {
			baselineRows := collectSearchDisplayRowsGo("", managers)
			if len(baselineRows) > 0 {
				baselineFile = filepath.Join(tmpDir, "reload-fallback.tsv")
				if err := writeDisplayRows(baselineFile, baselineRows); err == nil {
					reloadFallbackFile = baselineFile
				}
			}
		}
		fastManagerListCSV := strings.Join(dynamicReloadManagers(managers), ",")
		allManagerListCSV := strings.Join(managers, ",")
		reloadCmd = buildDynamicReloadCommandGo(input.ManagerOverride, reloadFallbackFile, fastManagerListCSV)
		reloadFullCmd = buildDynamicReloadCommandGo(input.ManagerOverride, reloadFallbackFile, allManagerListCSV)
		if dynamicReloadUseIPCGo() {
			reloadIPCCmd = buildDynamicQueryNotifyIPCCommandGo(input.ManagerOverride, reloadFallbackFile, fastManagerListCSV)
		}
	}

	// excludeSlowManagers is true when dynamic reload is enabled and the fast managers are a strict subset of all managers
	dynamicManagers := dynamicReloadManagers(managers)
	excludeSlowManagers := false
	if reloadCmd != "" && reloadFullCmd != "" {
		// Check if dynamicManagers is a strict subset: every element exists in managers AND length differs
		if len(dynamicManagers) < len(managers) {
			isSubset := true
			managerSet := make(map[string]bool, len(managers))
			for _, m := range managers {
				managerSet[m] = true
			}
			for _, dm := range dynamicManagers {
				if !managerSet[dm] {
					isSubset = false
					break
				}
			}
			excludeSlowManagers = isSubset
		}
	}

	selected, err := runFuzzySelectorGo(query, displayFile, header, helpFile, keybindFile, reloadCmd, reloadFullCmd, reloadIPCCmd, tmpDir, excludeSlowManagers)
	if err != nil {
		for _, row := range displayRows {
			fmt.Printf("%s\t%s\t%s\n", row.Manager, row.Package, row.Desc)
		}
		fmt.Fprintf(os.Stderr, "Interactive selection unavailable (%v). Showing results in feed format.\n", err)
		return 0
	}
	selected = strings.TrimSpace(selected)
	if selected == "" {
		fmt.Fprintln(os.Stderr, "Selection canceled")
		return 0
	}

	lines := strings.Split(strings.ReplaceAll(selected, "\r\n", "\n"), "\n")
	selectedManagers := make([]string, 0, len(lines))
	selectedPackages := make([]string, 0, len(lines))
	lineNumber := 0
	for _, line := range lines {
		lineNumber++
		rawLine := line
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 2 {
			logSelectionParseSkipGo(lineNumber, "missing manager/package fields", rawLine)
			continue
		}
		mgr := normalizeManagerName(strings.TrimSpace(parts[0]))
		pkg := strings.TrimSpace(parts[1])
		if mgr == "" || pkg == "" {
			logSelectionParseSkipGo(lineNumber, "missing manager/package fields", rawLine)
			continue
		}
		if !isManagerSupported(mgr) {
			logSelectionParseSkipGo(lineNumber, fmt.Sprintf("unsupported manager '%s'", mgr), rawLine)
			continue
		}
		selectedManagers = append(selectedManagers, mgr)
		selectedPackages = append(selectedPackages, pkg)
	}
	if len(selectedPackages) == 0 {
		fmt.Fprintln(os.Stderr, "Selection canceled")
		return 0
	}

	uniqueManagers := make([]string, 0)
	seenMgr := map[string]struct{}{}
	for _, mgr := range selectedManagers {
		if _, ok := seenMgr[mgr]; ok {
			continue
		}
		seenMgr[mgr] = struct{}{}
		uniqueManagers = append(uniqueManagers, mgr)
	}

	selectedDisplay := joinManagerLabelsGo(uniqueManagers)
	switch input.Action {
	case actionSearch:
		if !confirmActionGo(input.AssumeYes, fmt.Sprintf("Install %d package(s) with %s?", len(selectedPackages), selectedDisplay)) {
			fmt.Fprintln(os.Stderr, "Install canceled")
			return 0
		}
		for _, mgr := range uniqueManagers {
			pkgs := make([]string, 0)
			for i := range selectedPackages {
				if selectedManagers[i] == mgr {
					pkgs = append(pkgs, selectedPackages[i])
				}
			}
			if len(pkgs) == 0 {
				continue
			}
			fmt.Fprintf(os.Stderr, "Installing %d package(s) with %s\n", len(pkgs), managerLabelGo(mgr))
			if err := executeManagerAction(managerActionInput{Action: "install", Manager: mgr, Packages: pkgs}); err != nil {
				fmt.Fprintf(os.Stderr, "fpf-go: %v\n", err)
				return 1
			}
		}
	case actionRemove:
		if !confirmActionGo(input.AssumeYes, fmt.Sprintf("Remove %d package(s) with %s?", len(selectedPackages), selectedDisplay)) {
			fmt.Fprintln(os.Stderr, "Remove canceled")
			return 0
		}
		for _, mgr := range uniqueManagers {
			pkgs := make([]string, 0)
			for i := range selectedPackages {
				if selectedManagers[i] == mgr {
					pkgs = append(pkgs, selectedPackages[i])
				}
			}
			if len(pkgs) == 0 {
				continue
			}
			fmt.Fprintf(os.Stderr, "Removing %d package(s) with %s\n", len(pkgs), managerLabelGo(mgr))
			if err := executeManagerAction(managerActionInput{Action: "remove", Manager: mgr, Packages: pkgs}); err != nil {
				fmt.Fprintf(os.Stderr, "fpf-go: %v\n", err)
				return 1
			}
		}
	case actionList:
		for i := range selectedPackages {
			fmt.Printf("\n=== %s (%s) ===\n", selectedPackages[i], managerLabelGo(selectedManagers[i]))
			_ = executeManagerAction(managerActionInput{Action: "show_info", Manager: selectedManagers[i], Packages: []string{selectedPackages[i]}})
		}
	}

	return 0
}

func parseCLIInput(args []string) (cliInput, error) {
	input := cliInput{Action: actionSearch}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			if i+1 < len(args) {
				input.QueryParts = append(input.QueryParts, args[i+1:]...)
			}
			break
		}
		switch arg {
		case "-h", "--help":
			input.Action = actionHelp
		case "-v", "--version":
			input.Action = actionVersion
		case "-l", "--list-installed":
			input.Action = actionList
		case "-R", "--remove":
			input.Action = actionRemove
		case "-U", "--update":
			input.Action = actionUpdate
		case "--refresh":
			input.Action = actionRefresh
		case "--feed-search":
			input.Action = actionFeed
		case "-y", "--yes":
			input.AssumeYes = true
		case "-ap", "--apt":
			input.ManagerOverride = "apt"
		case "-dn", "--dnf":
			input.ManagerOverride = "dnf"
		case "-pm", "--pacman":
			input.ManagerOverride = "pacman"
		case "-zy", "--zypper":
			input.ManagerOverride = "zypper"
		case "-em", "--emerge":
			input.ManagerOverride = "emerge"
		case "-br", "--brew":
			input.ManagerOverride = "brew"
		case "-wg", "--winget":
			input.ManagerOverride = "winget"
		case "-ch", "--choco":
			input.ManagerOverride = "choco"
		case "-sc", "--scoop":
			input.ManagerOverride = "scoop"
		case "-sn", "--snap":
			input.ManagerOverride = "snap"
		case "-fp", "--flatpak":
			input.ManagerOverride = "flatpak"
		case "-np", "--npm":
			input.ManagerOverride = "npm"
		case "-bn", "--bun":
			input.ManagerOverride = "bun"
		case "-ad", "--auto":
			input.ManagerOverride = ""
		case "-m", "--manager":
			if i+1 >= len(args) {
				return input, fmt.Errorf("Missing value for --manager")
			}
			next := strings.TrimSpace(args[i+1])
			if next == "" || strings.HasPrefix(next, "-") {
				return input, fmt.Errorf("Missing value for --manager")
			}
			input.ManagerOverride = normalizeManagerName(next)
			i++
		default:
			if strings.HasPrefix(arg, "--manager=") {
				value := strings.TrimSpace(strings.TrimPrefix(arg, "--manager="))
				if value == "" || strings.HasPrefix(value, "-") {
					return input, fmt.Errorf("Missing value for --manager")
				}
				input.ManagerOverride = normalizeManagerName(value)
			} else if strings.HasPrefix(arg, "-") {
				return input, fmt.Errorf("Invalid option: %s", arg)
			} else {
				input.QueryParts = append(input.QueryParts, arg)
			}
		}
	}
	return input, nil
}

func collectSearchDisplayRowsGo(query string, managers []string) []displayRow {
	rows, err := buildDisplayRows(query, managers)
	if err != nil || len(rows) == 0 {
		return nil
	}
	return displayRowsFromBuildRows(rows)
}

func collectInstalledDisplayRowsGo(managers []string) []displayRow {
	rows := make([]displayRow, 0)
	for _, manager := range managers {
		names, err := executeInstalledEntries(installedInput{Manager: manager})
		if err != nil {
			continue
		}
		for _, name := range names {
			if name == "" {
				continue
			}
			rows = append(rows, displayRow{Manager: manager, Package: name, Desc: "installed"})
		}
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
	out := make([]displayRow, 0, len(rows))
	for _, row := range rows {
		key := row.Manager + "\t" + row.Package
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, row)
	}
	return out
}

func parseDisplayRows(raw []byte) []displayRow {
	rows := make([]displayRow, 0)
	for _, line := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 2 {
			continue
		}
		desc := "-"
		if len(parts) == 3 && parts[2] != "" {
			desc = parts[2]
		}
		rows = append(rows, displayRow{Manager: parts[0], Package: parts[1], Desc: desc})
	}
	return rows
}

func displayRowsFromBuildRows(rows []buildDisplayRow) []displayRow {
	out := make([]displayRow, 0, len(rows))
	for _, row := range rows {
		desc := row.Desc
		if desc == "" {
			desc = "-"
		}
		out = append(out, displayRow{
			Manager: row.Manager,
			Package: row.Package,
			Desc:    desc,
		})
	}
	return out
}

func writeDisplayRows(path string, rows []displayRow) error {
	if path == "" {
		return fmt.Errorf("path required")
	}
	if len(path) > 4096 {
		return fmt.Errorf("path too long")
	}
	if !filepath.IsAbs(path) {
		return fmt.Errorf("path must be absolute: %q", path)
	}
	if strings.Contains(path, "\x00") || strings.Contains(path, "..") {
		return fmt.Errorf("invalid path: %q", path)
	}
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("path is symlink: %q", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("mkdir for %q: %w", path, err)
	}
	var b strings.Builder
	for _, row := range rows {
		if row.Manager == "" || row.Package == "" {
			continue
		}
		if len(row.Manager) > 32 || len(row.Package) > maxPackageLength {
			continue
		}
		if !isManagerSupported(row.Manager) {
			continue
		}
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
		return fmt.Errorf("display rows too large: %d", len(content))
	}
	return os.WriteFile(path, []byte(content), 0o600)
}

func isSafeSessionPath(path string) bool {
	if path == "" || len(path) > 4096 || strings.Contains(path, "\x00") {
		return false
	}
	if !filepath.IsAbs(path) {
		return false
	}
	if strings.Contains(path, "..") {
		return false
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return false
		}
	}
	cleaned := filepath.Clean(path)
	tmpRaw := os.TempDir()
	if tmpRaw == "" {
		tmpRaw = "/tmp"
	}
	tempDir := filepath.Clean(tmpRaw)
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = strings.TrimSpace(os.Getenv("HOME"))
	}
	home = filepath.Clean(home)
	isUnder := func(base, p string) bool {
		if base == "" || base == "." {
			return false
		}
		if p == base {
			return true
		}
		if strings.HasPrefix(p, base+string(os.PathSeparator)) {
			return true
		}
		return false
	}
	if isUnder(tempDir, cleaned) {
		return true
	}
	if home != "" && home != "." && isUnder(home, cleaned) {
		return true
	}
	return false
}
