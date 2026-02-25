package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
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
	managers := resolveManagers(input.ManagerOverride, input.Action, query)
	if len(managers) == 0 {
		fmt.Fprintln(os.Stderr, "Unable to auto-detect supported package managers. Use --manager.")
		return 1
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
	tmpDir := ""
	if sessionOverride != "" {
		if err := os.MkdirAll(sessionOverride, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "fpf-go: %v\n", err)
			return 1
		}
		tmpDir = sessionOverride
	} else {
		tmpRoot := os.TempDir()
		sessionBase := filepath.Join(tmpRoot, "fpf")
		if mkErr := os.MkdirAll(sessionBase, 0o755); mkErr != nil {
			fmt.Fprintf(os.Stderr, "fpf-go: %v\n", mkErr)
			return 1
		}
		created, err := os.MkdirTemp(sessionBase, "session.")
		if err != nil {
			fmt.Fprintf(os.Stderr, "fpf-go: %v\n", err)
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
	_ = os.WriteFile(helpFile, []byte(buildHelpTextGo(managers)), 0o644)
	_ = os.WriteFile(keybindFile, []byte(buildKeybindTextGo()), 0o644)

	reloadCmd := ""
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
		managerListCSV := strings.Join(managers, ",")
		reloadCmd = buildDynamicReloadCommandGo(input.ManagerOverride, reloadFallbackFile, managerListCSV)
		if dynamicReloadUseIPCGo() {
			reloadIPCCmd = buildDynamicQueryNotifyIPCCommandGo(input.ManagerOverride, reloadFallbackFile, managerListCSV)
		}
	}

	selected, err := runFuzzySelectorGo(query, displayFile, header, helpFile, keybindFile, reloadCmd, reloadIPCCmd, tmpDir)
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

func containsArg(args []string, target string) bool {
	for _, arg := range args {
		if arg == target {
			return true
		}
	}
	return false
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

func resolveManagers(override string, action cliAction, query string) []string {
	stageStart := time.Now()
	defer logPerfTraceStage("manager-resolve", stageStart)

	if override != "" {
		override = normalizeManagerName(override)
		if isManagerSupported(override) && isManagerCommandReady(override) {
			return []string{override}
		}
		return nil
	}
	includeNpmWithBun := (action == actionSearch || action == actionFeed) && strings.TrimSpace(query) != ""
	return detectDefaultManagersGo(includeNpmWithBun)
}

func detectDefaultManagersGo(includeNpmWithBun bool) []string {
	primary := detectDefaultManagerGo()
	out := make([]string, 0)
	seen := map[string]struct{}{}
	add := func(m string) {
		if m == "" {
			return
		}
		if _, ok := seen[m]; ok {
			return
		}
		if !isManagerCommandReady(m) {
			return
		}
		seen[m] = struct{}{}
		out = append(out, m)
	}
	add(primary)
	preferBun := !includeNpmWithBun && isManagerCommandReady("bun")
	all := []string{"apt", "dnf", "pacman", "zypper", "emerge", "brew", "winget", "choco", "scoop", "snap", "flatpak", "bun", "npm"}
	for _, m := range all {
		if preferBun && m == "npm" {
			continue
		}
		add(m)
	}
	return out
}

func detectDefaultManagerGo() string {
	osName := testableGoOS()
	if osName == "darwin" && isManagerCommandReady("brew") {
		return "brew"
	}
	if osName == "windows" {
		for _, m := range []string{"winget", "choco", "scoop", "bun", "npm"} {
			if isManagerCommandReady(m) {
				return m
			}
		}
	}
	if osName == "linux" {
		for _, m := range []string{"apt", "dnf", "pacman", "zypper", "emerge", "snap", "flatpak", "bun", "npm"} {
			if isManagerCommandReady(m) {
				return m
			}
		}
	}
	for _, m := range []string{"brew", "winget", "choco", "scoop", "bun", "npm"} {
		if isManagerCommandReady(m) {
			return m
		}
	}
	return ""
}

func testableGoOS() string {
	if mock := strings.TrimSpace(os.Getenv("FPF_TEST_UNAME")); mock != "" {
		m := strings.ToLower(mock)
		switch {
		case strings.Contains(m, "darwin"):
			return "darwin"
		case strings.Contains(m, "mingw"), strings.Contains(m, "msys"), strings.Contains(m, "cygwin"), strings.Contains(m, "windows"):
			return "windows"
		case strings.Contains(m, "linux"):
			return "linux"
		}
	}
	return runtime.GOOS
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
	var b strings.Builder
	for _, row := range rows {
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
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func runFuzzySelectorGo(query, inputFile, header, helpFile, keybindFile, reloadCmd, reloadIPCCmd, sessionTmp string) (string, error) {
	stageStart := time.Now()
	defer logPerfTraceStage("fzf", stageStart)

	scriptPath := os.Args[0]
	previewCmd := fmt.Sprintf("FPF_SESSION_TMP_ROOT=%s %s --preview-item --manager {1} -- {2}", shellQuote(sessionTmp), shellQuote(scriptPath))
	args := []string{
		"-q", query,
		"-m",
		"-e",
		"--delimiter=\t",
		"--with-nth=1,2,3",
		"--preview=" + previewCmd,
		"--preview-window=55%:wrap:border-sharp",
		"--layout=reverse",
		"--marker=>>",
		"--prompt=Search> ",
		"--header=" + header,
		"--info=inline",
		"--margin=2%,1%,2%,1%",
		"--cycle",
		"--tiebreak=begin,chunk,length",
		"--bind=ctrl-k:preview:cat " + shellQuote(keybindFile),
		"--bind=ctrl-h:preview:cat " + shellQuote(helpFile),
		"--bind=ctrl-/:change-preview-window(hidden|)",
		"--bind=ctrl-n:next-selected,ctrl-b:prev-selected",
		"--bind=focus:transform-preview-label:echo [{1}] {2}",
	}

	if reloadIPCCmd != "" {
		args = append(args, "--listen=0")
		args = append(args, "--bind=change:execute-silent:"+reloadIPCCmd)
		if reloadCmd != "" {
			if fzfSupportsResultBindGo() {
				args = append(args, "--bind=ctrl-r:change-prompt(Loading> )+reload:"+reloadCmd)
				args = append(args, "--bind=result:change-prompt(Search> )")
			} else {
				args = append(args, "--bind=ctrl-r:reload:"+reloadCmd)
			}
		}
	} else if reloadCmd != "" {
		if fzfSupportsResultBindGo() {
			args = append(args, "--bind=change:change-prompt(Loading> )+reload:"+reloadCmd)
			args = append(args, "--bind=ctrl-r:change-prompt(Loading> )+reload:"+reloadCmd)
			args = append(args, "--bind=result:change-prompt(Search> )")
		} else {
			args = append(args, "--bind=change:reload:"+reloadCmd)
			args = append(args, "--bind=ctrl-r:reload:"+reloadCmd)
		}
	}

	stdinFile, err := os.Open(inputFile)
	if err != nil {
		return "", err
	}
	defer stdinFile.Close()

	cmd := exec.Command("fzf", args...)
	cmd.Env = append(os.Environ(), "SHELL=bash")
	cmd.Stdin = stdinFile
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	useDirectStderr := stderrHasTerminalGo()
	var stderr bytes.Buffer
	if useDirectStderr {
		cmd.Stderr = os.Stderr
	} else {
		cmd.Stderr = &stderr
	}

	err = cmd.Run()
	if err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if !ok {
			return "", err
		}
		switch exitErr.ExitCode() {
		case 1, 130:
			return "", nil
		}
		if !useDirectStderr {
			stderrText := strings.TrimSpace(stderr.String())
			if stderrText != "" {
				return "", fmt.Errorf("fzf exited with code %d: %s", exitErr.ExitCode(), stderrText)
			}
		}
		return "", fmt.Errorf("fzf exited with code %d", exitErr.ExitCode())
	}
	return stdout.String(), nil
}

func stderrHasTerminalGo() bool {
	info, err := os.Stderr.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func buildDynamicReloadCommandGo(managerOverride, fallbackFile, managerListCSV string) string {
	bypass := dynamicReloadBypassValueGo()
	parts := []string{
		"FPF_SKIP_INSTALLED_MARKERS=1",
		"FPF_BYPASS_QUERY_CACHE=" + bypass,
		"FPF_SKIP_QUERY_CACHE_WRITE=1",
		"FPF_IPC_MANAGER_OVERRIDE=" + shellQuoteIfNeeded(managerOverride),
		"FPF_IPC_MANAGER_LIST=" + shellQuoteIfNeeded(managerListCSV),
		"FPF_IPC_FALLBACK_FILE=" + shellQuoteIfNeeded(fallbackFile),
		shellQuote(os.Args[0]),
		"--dynamic-reload",
		"--",
		"\"{q}\"",
	}
	return strings.Join(parts, " ")
}

func buildDynamicQueryNotifyIPCCommandGo(managerOverride, fallbackFile, managerListCSV string) string {
	parts := []string{
		"FPF_IPC_MANAGER_OVERRIDE=" + shellQuoteIfNeeded(managerOverride),
		"FPF_IPC_MANAGER_LIST=" + shellQuoteIfNeeded(managerListCSV),
		"FPF_IPC_FALLBACK_FILE=" + shellQuoteIfNeeded(fallbackFile),
		shellQuote(os.Args[0]),
		"--ipc-query-notify",
		"--",
		"\"{q}\"",
	}
	return strings.Join(parts, " ")
}

func shellQuoteIfNeeded(value string) string {
	if value == "" {
		return "''"
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			continue
		}
		switch r {
		case '_', '-', '.', '/', ':', ',':
			continue
		}
		return shellQuote(value)
	}
	return value
}

func dynamicReloadBypassValueGo() string {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("FPF_DYNAMIC_RELOAD_BYPASS_QUERY_CACHE")))
	switch v {
	case "0", "false", "no", "off":
		return "0"
	default:
		return "1"
	}
}

func dynamicReloadEnabledGo(managerCount int) bool {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("FPF_DYNAMIC_RELOAD")))
	switch mode {
	case "", "always", "auto", "on", "1", "true", "yes":
		return true
	case "never", "off", "0", "false", "no":
		return false
	case "single":
		return managerCount == 1
	default:
		return true
	}
}

func dynamicReloadUseIPCGo() bool {
	transport := strings.ToLower(strings.TrimSpace(os.Getenv("FPF_DYNAMIC_RELOAD_TRANSPORT")))
	switch transport {
	case "ipc", "listen", "http", "auto":
		return fzfSupportsListenGo()
	default:
		return false
	}
}

func fzfSupportsListenGo() bool {
	cmd := exec.Command("fzf", "--help")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "--listen")
}

func fzfSupportsResultBindGo() bool {
	cmd := exec.Command("fzf", "--bind=result:abort", "--filter", "probe")
	cmd.Stdin = strings.NewReader("probe\n")
	out, _ := cmd.CombinedOutput()
	return !strings.Contains(string(out), "unsupported key: result")
}

func managerLabelGo(manager string) string {
	labels := map[string]string{
		"apt": "APT", "dnf": "DNF", "pacman": "Pacman", "zypper": "Zypper", "emerge": "Portage (emerge)",
		"brew": "Homebrew", "winget": "WinGet", "choco": "Chocolatey", "scoop": "Scoop", "snap": "Snap",
		"flatpak": "Flatpak", "npm": "npm", "bun": "bun",
	}
	if label, ok := labels[manager]; ok {
		return label
	}
	return manager
}

func joinManagerLabelsGo(managers []string) string {
	parts := make([]string, 0, len(managers))
	for _, m := range managers {
		parts = append(parts, managerLabelGo(m))
	}
	return strings.Join(parts, ", ")
}

func confirmActionGo(assumeYes bool, prompt string) bool {
	if assumeYes || assumeYesEnvGo() {
		return true
	}
	fmt.Fprintf(os.Stderr, "%s [y/N]: ", prompt)
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes"
}

func assumeYesEnvGo() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("FPF_ASSUME_YES")))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

func buildHelpTextGo(managers []string) string {
	defaultManagers := joinManagerLabelsGo(managers)
	if defaultManagers == "" {
		defaultManagers = "None"
	}
	return "fpf - fuzzy package finder\n\n" +
		"Syntax:\n" +
		"  fpf [manager option] [action option] [query]\n\n" +
		"Detected default manager(s):\n" +
		"  " + defaultManagers + "\n\n" +
		"Action options:\n" +
		"  -l, --list-installed\n" +
		"  -R, --remove\n" +
		"  -U, --update\n" +
		"  --refresh\n" +
		"  -y, --yes\n" +
		"  -v, --version\n" +
		"  -h, --help\n"
}

func buildKeybindTextGo() string {
	return "Keybinds:\n\n  ctrl-h  Show help in preview pane\n  ctrl-k  Show keybinds in preview pane\n  ctrl-/  Toggle preview pane\n  ctrl-n  Move to next selected package\n  ctrl-b  Move to previous selected package\n"
}

func printCLIHelp() {
	fmt.Print(buildHelpTextGo(detectDefaultManagersGo(true)))
}

func managerNoQuerySetupMessageGo(manager string) string {
	switch manager {
	case "flatpak":
		if !flatpakHasFlathubRemoteGo() {
			_ = ensureFlatpakFlathubRemoteGo()
		}
		if !flatpakHasFlathubRemoteGo() {
			return "Flatpak has no remotes configured. Add Flathub with: flatpak remote-add --if-not-exists --user flathub https://flathub.org/repo/flathub.flatpakrepo"
		}
	case "winget":
		if !wingetHasDefaultSourceGo() {
			return "WinGet source 'winget' is not configured. Restore it with: winget source reset --force"
		}
	case "choco":
		if !chocoHasAnySourcesGo() {
			return "Chocolatey has no package sources configured. Add the default source with: choco source add -n=chocolatey -s=https://community.chocolatey.org/api/v2/"
		}
	case "scoop":
		if !scoopHasAnyBucketsGo() {
			return "Scoop has no buckets configured. Add the default bucket with: scoop bucket add main"
		}
	}
	return ""
}

func flatpakHasFlathubRemoteGo() bool {
	if !isManagerCommandReady("flatpak") {
		return false
	}
	for _, args := range [][]string{{"remotes", "--columns=name"}, {"remote-list", "--columns=name"}} {
		out, err := runOutputQuietErr("flatpak", args...)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(strings.ReplaceAll(string(out), "\r\n", "\n"), "\n") {
			fields := strings.Fields(line)
			if len(fields) == 0 {
				continue
			}
			name := strings.ToLower(strings.TrimSpace(fields[0]))
			if name == "flathub" {
				return true
			}
		}
	}
	return false
}

func ensureFlatpakFlathubRemoteGo() bool {
	if !isManagerCommandReady("flatpak") {
		return false
	}
	if flatpakHasFlathubRemoteGo() {
		return true
	}
	if err := runCommand("flatpak", "remote-add", "--if-not-exists", "--user", "flathub", "https://flathub.org/repo/flathub.flatpakrepo"); err != nil {
		_ = runRootCommand("flatpak", "remote-add", "--if-not-exists", "flathub", "https://flathub.org/repo/flathub.flatpakrepo")
	}
	return flatpakHasFlathubRemoteGo()
}

func wingetHasDefaultSourceGo() bool {
	if !isManagerCommandReady("winget") {
		return false
	}
	out, err := runOutputQuietErr("winget", "source", "list")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(strings.ReplaceAll(string(out), "\r\n", "\n"), "\n") {
		trim := strings.TrimSpace(line)
		if trim == "" {
			continue
		}
		fields := strings.Fields(trim)
		if len(fields) > 0 && strings.EqualFold(fields[0], "winget") {
			return true
		}
	}
	return false
}

func chocoHasAnySourcesGo() bool {
	if !isManagerCommandReady("choco") {
		return false
	}
	out, err := runOutputQuietErr("choco", "source", "list", "--limit-output")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(strings.ReplaceAll(string(out), "\r\n", "\n"), "\n") {
		parts := strings.Split(line, "|")
		if len(parts) >= 2 && strings.TrimSpace(parts[0]) != "" {
			return true
		}
	}
	return false
}

func scoopHasAnyBucketsGo() bool {
	if !isManagerCommandReady("scoop") {
		return false
	}
	out, err := runOutputQuietErr("scoop", "bucket", "list")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(strings.ReplaceAll(string(out), "\r\n", "\n"), "\n") {
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(strings.ToLower(trim), "name ") {
			continue
		}
		if strings.Trim(trim, "- ") == "" {
			continue
		}
		cols := strings.Fields(trim)
		if len(cols) >= 2 {
			c := cols[1]
			if strings.HasPrefix(c, "http://") || strings.HasPrefix(c, "https://") || strings.HasPrefix(c, "git@") || strings.HasPrefix(c, "ssh://") || strings.HasPrefix(c, "file://") || strings.HasPrefix(c, "/") {
				return true
			}
		}
	}
	return false
}

func selectionDebugEnabledGo() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("FPF_DEBUG_SELECTION")))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

func logSelectionParseSkipGo(lineNumber int, reason string, rawLine string) {
	if !selectionDebugEnabledGo() {
		return
	}
	fmt.Fprintf(os.Stderr, "Debug(selection): skipped line %d: %s; raw=%s\n", lineNumber, reason, rawLine)
}

func commandExistsGo(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func fzfCommandAvailableGo() bool {
	if strings.TrimSpace(os.Getenv("FPF_TEST_FORCE_FZF_MISSING")) == "1" {
		mockBin := strings.TrimSpace(os.Getenv("FPF_TEST_MOCK_BIN"))
		if mockBin != "" {
			candidate := filepath.Join(mockBin, "fzf")
			if info, err := os.Stat(candidate); err == nil && info.Mode()&0o111 != 0 {
				return true
			}
		}
		return false
	}
	return commandExistsGo("fzf")
}

func managerCanInstallFzfGo(manager string) bool {
	switch manager {
	case "apt", "dnf", "pacman", "zypper", "emerge", "brew", "winget", "choco", "scoop", "snap":
		return true
	default:
		return false
	}
}

func installFzfWithManagerGo(manager string) error {
	if strings.TrimSpace(os.Getenv("FPF_TEST_FZF_MANAGER_INSTALL_FAIL")) == "1" {
		return fmt.Errorf("forced manager install failure")
	}
	switch manager {
	case "apt":
		return runRootCommand("apt-get", "install", "-y", "fzf")
	case "dnf":
		return runRootCommand("dnf", "install", "-y", "fzf")
	case "pacman":
		return runRootCommand("pacman", "-S", "--needed", "fzf")
	case "zypper":
		return runRootCommand("zypper", "--non-interactive", "install", "--auto-agree-with-licenses", "fzf")
	case "emerge":
		return runRootCommand("emerge", "--ask=n", "app-shells/fzf")
	case "brew":
		return runCommand("brew", "install", "fzf")
	case "winget":
		if err := runCommand("winget", "install", "--id", "junegunn.fzf", "--exact", "--source", "winget", "--accept-package-agreements", "--accept-source-agreements", "--disable-interactivity"); err != nil {
			return runCommand("winget", "install", "--id", "fzf", "--exact", "--source", "winget", "--accept-package-agreements", "--accept-source-agreements", "--disable-interactivity")
		}
		return nil
	case "choco":
		return runCommand("choco", "install", "fzf", "-y")
	case "scoop":
		return runCommand("scoop", "install", "fzf")
	case "snap":
		return runRootCommand("snap", "install", "fzf")
	default:
		return fmt.Errorf("unsupported manager for fzf install")
	}
}

func installFzfFromReleaseFallbackGo() bool {
	if strings.TrimSpace(os.Getenv("FPF_TEST_BOOTSTRAP_FZF_FALLBACK")) == "1" {
		mockCmdPath := strings.TrimSpace(os.Getenv("FPF_TEST_MOCKCMD_PATH"))
		mockBin := strings.TrimSpace(os.Getenv("FPF_TEST_MOCK_BIN"))
		if mockCmdPath == "" || mockBin == "" {
			return false
		}
		target := filepath.Join(mockBin, "fzf")
		_ = os.Remove(target)
		if err := os.Symlink(mockCmdPath, target); err != nil {
			return false
		}
		return true
	}
	return false
}

func buildFzfBootstrapCandidatesGo(managers []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0)
	add := func(manager string) {
		if manager == "" || !managerCanInstallFzfGo(manager) || !isManagerCommandReady(manager) {
			return
		}
		if _, ok := seen[manager]; ok {
			return
		}
		seen[manager] = struct{}{}
		out = append(out, manager)
	}
	for _, m := range managers {
		add(m)
	}
	if len(out) > 0 {
		return out
	}
	for _, m := range []string{"apt", "dnf", "pacman", "zypper", "emerge", "brew", "winget", "choco", "scoop", "snap"} {
		add(m)
	}
	return out
}

func ensureFzfGo(managers []string) error {
	if fzfCommandAvailableGo() {
		return nil
	}
	candidates := buildFzfBootstrapCandidatesGo(managers)
	if len(candidates) == 0 {
		return fmt.Errorf("fzf is required and no compatible manager is available to auto-install it")
	}
	fmt.Fprintf(os.Stderr, "fzf is missing. Auto-installing with: %s\n", joinManagerLabelsGo(candidates))
	for _, manager := range candidates {
		fmt.Fprintf(os.Stderr, "Attempting fzf install with %s\n", managerLabelGo(manager))
		if err := installFzfWithManagerGo(manager); err == nil && fzfCommandAvailableGo() {
			return nil
		}
	}
	fmt.Fprintln(os.Stderr, "Package-manager bootstrap did not provide fzf. Trying release binary fallback.")
	if installFzfFromReleaseFallbackGo() && fzfCommandAvailableGo() {
		return nil
	}
	return fmt.Errorf("Failed to auto-install fzf. Install fzf manually and rerun.")
}
