package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func main() {
	if handled, exitCode := maybeRunGoMergeDisplay(os.Args[1:]); handled {
		os.Exit(exitCode)
	}

	if handled, exitCode := maybeRunGoRankDisplay(os.Args[1:]); handled {
		os.Exit(exitCode)
	}

	if handled, exitCode := maybeRunIPCQueryNotifyAction(os.Args[1:]); handled {
		os.Exit(exitCode)
	}

	if handled, exitCode := maybeRunIPCReloadAction(os.Args[1:]); handled {
		os.Exit(exitCode)
	}

	if handled, exitCode := maybeRunDynamicReloadAction(os.Args[1:]); handled {
		os.Exit(exitCode)
	}

	if handled, exitCode := maybeRunGoInstalledEntries(os.Args[1:]); handled {
		os.Exit(exitCode)
	}

	if handled, exitCode := maybeRunPreviewItemAction(os.Args[1:]); handled {
		os.Exit(exitCode)
	}

	if handled, exitCode := maybeRunGoSearchEntries(os.Args[1:]); handled {
		os.Exit(exitCode)
	}

	if handled, exitCode := maybeRunGoManagerAction(os.Args[1:]); handled {
		os.Exit(exitCode)
	}

	if hasMissingManagerValue(os.Args[1:]) {
		fmt.Fprintln(os.Stderr, "Missing value for --manager")
		os.Exit(1)
	}

	if isVersionRequest(os.Args[1:]) {
		version, err := resolvePackageVersion()
		if err != nil {
			fmt.Fprintf(os.Stderr, "fpf-go: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("fpf %s\n", version)
		return
	}

	scriptPath, err := resolveLegacyScriptPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "fpf-go: %v\n", err)
		os.Exit(1)
	}

	args := make([]string, 0, len(os.Args)+1)
	args = append(args, scriptPath)
	args = append(args, normalizeManagerArgs(os.Args[1:])...)

	cmd := exec.Command("bash", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(),
		"FPF_SELF_PATH="+os.Args[0],
		"FPF_SKIP_GO_BOOTSTRAP=1",
		"FPF_USE_GO_MANAGER_ACTIONS=1",
		"FPF_USE_GO_SEARCH_ENTRIES=1",
		"FPF_USE_GO_INSTALLED_ENTRIES=1",
		"FPF_USE_GO_DISPLAY_PIPELINE=1",
	)

	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.ExitCode())
		}

		fmt.Fprintf(os.Stderr, "fpf-go: failed to execute legacy fpf script: %v\n", err)
		os.Exit(1)
	}
}

func hasMissingManagerValue(args []string) bool {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			return false
		}

		switch {
		case arg == "-m" || arg == "--manager":
			if i+1 >= len(args) {
				return true
			}

			next := strings.TrimSpace(args[i+1])
			if next == "" || strings.HasPrefix(next, "-") {
				return true
			}

			i++
		case strings.HasPrefix(arg, "--manager="):
			if strings.TrimSpace(strings.TrimPrefix(arg, "--manager=")) == "" {
				return true
			}
		}
	}

	return false
}

func normalizeManagerArgs(args []string) []string {
	normalized := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			normalized = append(normalized, args[i:]...)
			return normalized
		}

		switch {
		case arg == "-m" || arg == "--manager":
			normalized = append(normalized, arg)
			if i+1 < len(args) {
				next := args[i+1]
				normalized = append(normalized, normalizeManagerName(next))
				i++
			}
		case strings.HasPrefix(arg, "--manager="):
			value := strings.TrimPrefix(arg, "--manager=")
			normalized = append(normalized, "--manager="+normalizeManagerName(value))
		default:
			normalized = append(normalized, arg)
		}
	}

	return normalized
}

func normalizeManagerName(value string) string {
	manager := strings.ToLower(strings.TrimSpace(value))
	manager = strings.Join(strings.Fields(manager), " ")

	switch manager {
	case "homebrew":
		return "brew"
	case "chocolatey", "chocolate":
		return "choco"
	case "portage (emerge)", "portage-emerge", "portage":
		return "emerge"
	case "win-get":
		return "winget"
	default:
		return manager
	}
}

func resolveLegacyScriptPath() (string, error) {
	if path := os.Getenv("FPF_LEGACY_SCRIPT"); path != "" {
		abs, err := filepath.Abs(path)
		if err != nil {
			return "", fmt.Errorf("invalid FPF_LEGACY_SCRIPT path %q: %w", path, err)
		}
		if err := ensureExecutableFile(abs); err != nil {
			return "", err
		}
		return abs, nil
	}

	exePath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("cannot resolve executable path: %w", err)
	}

	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		exePath = filepath.Clean(exePath)
	}

	exeDir := filepath.Dir(exePath)
	candidates := []string{
		filepath.Join(exeDir, "fpf"),
		filepath.Join(exeDir, "..", "fpf"),
		filepath.Join(exeDir, "..", "..", "fpf"),
	}

	for _, candidate := range candidates {
		abs, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		if ensureExecutableFile(abs) == nil {
			return abs, nil
		}
	}

	cwd, err := os.Getwd()
	if err == nil {
		fallback := filepath.Join(cwd, "fpf")
		if ensureExecutableFile(fallback) == nil {
			return fallback, nil
		}
	}

	return "", errors.New("unable to locate legacy 'fpf' script; set FPF_LEGACY_SCRIPT to an executable path")
}

func ensureExecutableFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("legacy script not found at %q: %w", path, err)
	}
	if info.IsDir() {
		return fmt.Errorf("legacy script path %q is a directory", path)
	}
	if info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("legacy script at %q is not executable", path)
	}
	return nil
}

func isVersionRequest(args []string) bool {
	for _, arg := range args {
		if arg == "-v" || arg == "--version" {
			return true
		}
	}
	return false
}

func resolvePackageVersion() (string, error) {
	pkgPath, err := resolvePackageJSONPath()
	if err != nil {
		return "", err
	}

	raw, err := os.ReadFile(pkgPath)
	if err != nil {
		return "", fmt.Errorf("failed to read package.json at %q: %w", pkgPath, err)
	}

	payload := struct {
		Version string `json:"version"`
	}{}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", fmt.Errorf("failed to parse package.json at %q: %w", pkgPath, err)
	}
	if payload.Version == "" {
		return "", fmt.Errorf("package.json at %q does not include a version", pkgPath)
	}

	return payload.Version, nil
}

func resolvePackageJSONPath() (string, error) {
	if path := os.Getenv("FPF_PACKAGE_JSON"); path != "" {
		abs, err := filepath.Abs(path)
		if err != nil {
			return "", fmt.Errorf("invalid FPF_PACKAGE_JSON path %q: %w", path, err)
		}
		if _, err := os.Stat(abs); err != nil {
			return "", fmt.Errorf("package.json not found at %q: %w", abs, err)
		}
		return abs, nil
	}

	exePath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("cannot resolve executable path: %w", err)
	}

	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		exePath = filepath.Clean(exePath)
	}

	exeDir := filepath.Dir(exePath)
	candidates := []string{
		filepath.Join(exeDir, "package.json"),
		filepath.Join(exeDir, "..", "package.json"),
		filepath.Join(exeDir, "..", "..", "package.json"),
	}

	for _, candidate := range candidates {
		abs, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		if _, err := os.Stat(abs); err == nil {
			return abs, nil
		}
	}

	cwd, err := os.Getwd()
	if err == nil {
		fallback := filepath.Join(cwd, "package.json")
		if _, err := os.Stat(fallback); err == nil {
			return fallback, nil
		}
	}

	return "", errors.New("unable to locate package.json; set FPF_PACKAGE_JSON to an explicit path")
}

type managerActionInput struct {
	Action   string
	Manager  string
	Packages []string
}

func maybeRunGoManagerAction(args []string) (bool, int) {
	input, ok, err := parseManagerActionInput(args)
	if !ok {
		return false, 0
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "fpf-go: %v\n", err)
		return true, 2
	}

	if err := executeManagerAction(input); err != nil {
		fmt.Fprintf(os.Stderr, "fpf-go: %v\n", err)
		return true, 1
	}

	return true, 0
}

func parseManagerActionInput(args []string) (managerActionInput, bool, error) {
	input := managerActionInput{}
	if len(args) == 0 {
		return input, false, nil
	}

	hasFlag := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			if hasFlag {
				input.Packages = append(input.Packages, args[i+1:]...)
				break
			}
			return input, false, nil
		}

		switch arg {
		case "--go-manager-action":
			hasFlag = true
			if i+1 >= len(args) {
				return input, true, errors.New("missing value for --go-manager-action")
			}
			input.Action = strings.TrimSpace(args[i+1])
			i++
		case "--go-manager":
			hasFlag = true
			if i+1 >= len(args) {
				return input, true, errors.New("missing value for --go-manager")
			}
			input.Manager = strings.TrimSpace(args[i+1])
			i++
		}
	}

	if !hasFlag {
		return input, false, nil
	}
	if input.Action == "" {
		return input, true, errors.New("--go-manager-action is required")
	}
	if input.Manager == "" {
		return input, true, errors.New("--go-manager is required")
	}

	input.Manager = normalizeManagerName(input.Manager)
	return input, true, nil
}

func executeManagerAction(input managerActionInput) error {
	manager := input.Manager
	action := input.Action
	pkgs := input.Packages

	switch manager {
	case "apt":
		switch action {
		case "install":
			return runRootCommand("apt-get", append([]string{"install", "-y"}, pkgs...)...)
		case "remove":
			return runRootCommand("apt-get", append([]string{"remove", "-y"}, pkgs...)...)
		case "show_info":
			pkg := firstPackage(pkgs)
			runCommandQuietErr("apt-cache", "show", pkg)
			fmt.Println()
			runCommandQuietErr("dpkg", "-L", pkg)
			return nil
		case "update":
			if err := runRootCommand("apt-get", "update"); err != nil {
				return err
			}
			return runRootCommand("apt-get", "upgrade", "-y")
		case "refresh":
			return runRootCommand("apt-get", "update")
		}
	case "dnf":
		switch action {
		case "install":
			return runRootCommand("dnf", append([]string{"install", "-y"}, pkgs...)...)
		case "remove":
			return runRootCommand("dnf", append([]string{"remove", "-y"}, pkgs...)...)
		case "show_info":
			pkg := firstPackage(pkgs)
			runCommandQuietErr("dnf", "info", pkg)
			fmt.Println()
			runCommandQuietErr("rpm", "-ql", pkg)
			return nil
		case "update":
			return runRootCommand("dnf", "upgrade", "-y")
		case "refresh":
			return runRootCommand("dnf", "makecache")
		}
	case "pacman":
		switch action {
		case "install":
			return runRootCommand("pacman", append([]string{"-S", "--needed"}, pkgs...)...)
		case "remove":
			return runRootCommand("pacman", append([]string{"-Rsn"}, pkgs...)...)
		case "show_info":
			pkg := firstPackage(pkgs)
			if err := runCommandQuietErr("pacman", "-Qi", pkg); err != nil {
				runCommandQuietErr("pacman", "-Si", pkg)
			}
			fmt.Println()
			runCommandQuietErr("pacman", "-Ql", pkg)
			return nil
		case "update":
			return runRootCommand("pacman", "-Syu")
		case "refresh":
			return runRootCommand("pacman", "-Sy")
		}
	case "zypper":
		switch action {
		case "install":
			return runRootCommand("zypper", append([]string{"--non-interactive", "install", "--auto-agree-with-licenses"}, pkgs...)...)
		case "remove":
			return runRootCommand("zypper", append([]string{"--non-interactive", "remove"}, pkgs...)...)
		case "show_info":
			return runCommandQuietErr("zypper", "--non-interactive", "info", firstPackage(pkgs))
		case "update":
			if err := runRootCommand("zypper", "--non-interactive", "refresh"); err != nil {
				return err
			}
			return runRootCommand("zypper", "--non-interactive", "update")
		case "refresh":
			return runRootCommand("zypper", "--non-interactive", "refresh")
		}
	case "emerge":
		switch action {
		case "install":
			return runRootCommand("emerge", append([]string{"--ask=n", "--verbose"}, pkgs...)...)
		case "remove":
			if err := runRootCommand("emerge", append([]string{"--ask=n", "--deselect"}, pkgs...)...); err != nil {
				return err
			}
			return runRootCommand("emerge", append([]string{"--ask=n", "--depclean"}, pkgs...)...)
		case "show_info":
			return runCommandQuietErr("emerge", "--search", "--color=n", firstPackage(pkgs))
		case "update":
			if err := runRootCommand("emerge", "--sync"); err != nil {
				return err
			}
			return runRootCommand("emerge", "--ask=n", "--update", "--deep", "--newuse", "@world")
		case "refresh":
			return runRootCommand("emerge", "--sync")
		}
	case "brew":
		switch action {
		case "install":
			return runCommand("brew", append([]string{"install"}, pkgs...)...)
		case "remove":
			return runCommand("brew", append([]string{"uninstall"}, pkgs...)...)
		case "show_info":
			return runCommandQuietErr("brew", "info", firstPackage(pkgs))
		case "update":
			if err := runCommand("brew", "update"); err != nil {
				return err
			}
			return runCommand("brew", "upgrade")
		case "refresh":
			return runCommand("brew", "update")
		}
	case "winget":
		switch action {
		case "install":
			for _, pkg := range pkgs {
				if err := runCommand("winget", "install", "--id", pkg, "--exact", "--source", "winget", "--accept-package-agreements", "--accept-source-agreements", "--disable-interactivity"); err != nil {
					return err
				}
			}
			return nil
		case "remove":
			for _, pkg := range pkgs {
				if err := runCommand("winget", "uninstall", "--id", pkg, "--exact", "--source", "winget", "--disable-interactivity"); err != nil {
					return err
				}
			}
			return nil
		case "show_info":
			return runCommandQuietErr("winget", "show", "--id", firstPackage(pkgs), "--exact", "--source", "winget", "--accept-source-agreements", "--disable-interactivity")
		case "update":
			return runCommand("winget", "upgrade", "--all", "--source", "winget", "--accept-package-agreements", "--accept-source-agreements", "--disable-interactivity")
		case "refresh":
			return runCommand("winget", "source", "update", "--name", "winget", "--accept-source-agreements", "--disable-interactivity")
		}
	case "choco":
		switch action {
		case "install":
			return runCommand("choco", append([]string{"install"}, append(pkgs, "-y")...)...)
		case "remove":
			return runCommand("choco", append([]string{"uninstall"}, append(pkgs, "-y")...)...)
		case "show_info":
			return runCommandQuietErr("choco", "info", firstPackage(pkgs))
		case "update":
			return runCommand("choco", "upgrade", "all", "-y")
		case "refresh":
			cmd := exec.Command("choco", "source", "list", "--limit-output")
			cmd.Env = os.Environ()
			cmd.Stdin = os.Stdin
			cmd.Stdout = io.Discard
			cmd.Stderr = os.Stderr
			return cmd.Run()
		}
	case "scoop":
		switch action {
		case "install":
			return runCommand("scoop", append([]string{"install"}, pkgs...)...)
		case "remove":
			return runCommand("scoop", append([]string{"uninstall"}, pkgs...)...)
		case "show_info":
			return runCommandQuietErr("scoop", "info", firstPackage(pkgs))
		case "update":
			if err := runCommand("scoop", "update"); err != nil {
				return err
			}
			return runCommand("scoop", "update", "*")
		case "refresh":
			return runCommand("scoop", "update")
		}
	case "snap":
		switch action {
		case "install":
			for _, pkg := range pkgs {
				if err := runRootCommandQuietErr("snap", "install", pkg); err != nil {
					if err2 := runRootCommand("snap", "install", "--classic", pkg); err2 != nil {
						return err2
					}
				}
			}
			return nil
		case "remove":
			return runRootCommand("snap", append([]string{"remove"}, pkgs...)...)
		case "show_info":
			return runCommandQuietErr("snap", "info", firstPackage(pkgs))
		case "update":
			return runRootCommand("snap", "refresh")
		case "refresh":
			return runRootCommand("snap", "refresh", "--list")
		}
	case "flatpak":
		switch action {
		case "install":
			for _, pkg := range pkgs {
				if err := runCommandQuietErr("flatpak", "install", "-y", "--user", "flathub", pkg); err == nil {
					continue
				}
				if err := runCommandQuietErr("flatpak", "install", "-y", "--user", pkg); err == nil {
					continue
				}
				if err := runRootCommandQuietErr("flatpak", "install", "-y", "flathub", pkg); err == nil {
					continue
				}
				if err := runRootCommand("flatpak", "install", "-y", pkg); err != nil {
					return err
				}
			}
			return nil
		case "remove":
			if err := runCommandQuietErr("flatpak", append([]string{"uninstall", "-y", "--user"}, pkgs...)...); err != nil {
				return runRootCommand("flatpak", append([]string{"uninstall", "-y"}, pkgs...)...)
			}
			return nil
		case "show_info":
			if err := runCommandQuietErr("flatpak", "info", firstPackage(pkgs)); err != nil {
				return runCommandQuietErr("flatpak", "remote-info", "flathub", firstPackage(pkgs))
			}
			return nil
		case "update":
			if err := runCommandQuietErr("flatpak", "update", "-y", "--user"); err != nil {
				return runRootCommand("flatpak", "update", "-y")
			}
			return nil
		case "refresh":
			if err := runCommandQuietErr("flatpak", "update", "-y", "--appstream", "--user"); err != nil {
				return runRootCommand("flatpak", "update", "-y", "--appstream")
			}
			return nil
		}
	case "npm":
		switch action {
		case "install":
			return runCommand("npm", append([]string{"install", "-g"}, pkgs...)...)
		case "remove":
			return runCommand("npm", append([]string{"uninstall", "-g"}, pkgs...)...)
		case "show_info":
			return runCommandQuietErr("npm", "view", firstPackage(pkgs))
		case "update":
			return runCommand("npm", "update", "-g")
		case "refresh":
			return runCommand("npm", "cache", "verify")
		}
	case "bun":
		switch action {
		case "install":
			return runCommand("bun", append([]string{"add", "-g"}, pkgs...)...)
		case "remove":
			return runCommand("bun", append([]string{"remove", "--global"}, pkgs...)...)
		case "show_info":
			if err := runCommandQuietErr("bun", "info", firstPackage(pkgs)); err != nil {
				return runCommandQuietErr("npm", "view", firstPackage(pkgs))
			}
			return nil
		case "update":
			return runCommand("bun", "update", "--global")
		case "refresh":
			cmd := exec.Command("bun", "pm", "cache")
			cmd.Env = os.Environ()
			cmd.Stdin = os.Stdin
			cmd.Stdout = io.Discard
			cmd.Stderr = os.Stderr
			return cmd.Run()
		}
	}

	return fmt.Errorf("unsupported manager action: manager=%s action=%s", manager, action)
}

func firstPackage(pkgs []string) string {
	if len(pkgs) == 0 {
		return ""
	}
	return pkgs[0]
}

func runCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Env = os.Environ()
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func runCommandQuietErr(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Env = os.Environ()
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = io.Discard
	return cmd.Run()
}

func runRootCommand(name string, args ...string) error {
	if needsRoot(name) && os.Geteuid() != 0 {
		if _, err := exec.LookPath("sudo"); err != nil {
			return errors.New("requires root privileges and sudo was not found")
		}
		sudoArgs := append([]string{name}, args...)
		return runCommand("sudo", sudoArgs...)
	}
	return runCommand(name, args...)
}

func runRootCommandQuietErr(name string, args ...string) error {
	if needsRoot(name) && os.Geteuid() != 0 {
		if _, err := exec.LookPath("sudo"); err != nil {
			return errors.New("requires root privileges and sudo was not found")
		}
		sudoArgs := append([]string{name}, args...)
		return runCommandQuietErr("sudo", sudoArgs...)
	}
	return runCommandQuietErr(name, args...)
}

func needsRoot(managerBinary string) bool {
	switch managerBinary {
	case "apt-get", "dnf", "pacman", "zypper", "emerge", "snap":
		return true
	default:
		return false
	}
}

func maybeRunPreviewItemAction(args []string) (bool, int) {
	hasPreview, manager, packageName := parsePreviewRequest(args)
	if !hasPreview {
		return false, 0
	}
	if manager == "" || packageName == "" {
		return true, 0
	}

	sessionRoot := os.Getenv("FPF_SESSION_TMP_ROOT")
	if sessionRoot == "" {
		sessionRoot = os.TempDir()
	}

	cacheDir := filepath.Join(sessionRoot, "preview-cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "fpf-go: failed to create preview cache dir: %v\n", err)
		return true, 1
	}

	key, err := cksumKey(manager + "|" + packageName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fpf-go: failed to compute preview cache key: %v\n", err)
		return true, 1
	}
	cacheFile := filepath.Join(cacheDir, fmt.Sprintf("%s.%s.txt", manager, key))

	if raw, err := os.ReadFile(cacheFile); err == nil {
		_, _ = os.Stdout.Write(raw)
		return true, 0
	}

	raw, err := runShowInfoOutput(manager, packageName)
	if err != nil {
		return true, 0
	}

	tmpFile, err := os.CreateTemp(sessionRoot, "preview.")
	if err != nil {
		fmt.Fprintf(os.Stderr, "fpf-go: failed to create preview temp file: %v\n", err)
		return true, 1
	}
	if _, err := tmpFile.Write(raw); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpFile.Name())
		fmt.Fprintf(os.Stderr, "fpf-go: failed to write preview temp file: %v\n", err)
		return true, 1
	}
	_ = tmpFile.Close()
	if err := os.Rename(tmpFile.Name(), cacheFile); err != nil {
		_ = os.Remove(tmpFile.Name())
		fmt.Fprintf(os.Stderr, "fpf-go: failed to finalize preview cache file: %v\n", err)
		return true, 1
	}

	_, _ = os.Stdout.Write(raw)
	return true, 0
}

func parsePreviewRequest(args []string) (bool, string, string) {
	hasPreview := false
	manager := ""
	packageName := ""

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			if i+1 < len(args) {
				packageName = args[i+1]
			}
			break
		}
		switch arg {
		case "--preview-item":
			hasPreview = true
		case "--manager":
			if i+1 < len(args) {
				manager = normalizeManagerName(args[i+1])
				i++
			}
		}
	}

	return hasPreview, manager, packageName
}

func cksumKey(input string) (string, error) {
	cmd := exec.Command("cksum")
	cmd.Env = os.Environ()
	cmd.Stdin = strings.NewReader(input)
	cmd.Stderr = io.Discard
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return "", errors.New("empty cksum output")
	}
	return fields[0], nil
}

func runShowInfoOutput(manager string, packageName string) ([]byte, error) {
	cmd := exec.Command(
		os.Args[0],
		"--go-manager-action", "show_info",
		"--go-manager", manager,
		"--", packageName,
	)
	cmd.Env = append(os.Environ(), "FPF_USE_GO_MANAGER_ACTIONS=1")
	cmd.Stderr = io.Discard
	return cmd.Output()
}
