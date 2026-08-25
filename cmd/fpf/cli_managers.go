package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

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
	// Determine preference before adding primary, so npm can be excluded when bun is preferred
	preferBun := !includeNpmWithBun && isManagerCommandReady("bun")
	out := make([]string, 0)
	seen := map[string]struct{}{}
	add := func(m string) {
		if m == "" {
			return
		}
		if preferBun && m == "npm" {
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
	primary := detectDefaultManagerGo()
	// If primary is npm but we prefer bun, skip primary to honour preference
	if !(preferBun && primary == "npm") {
		add(primary)
	}
	all := []string{"apt", "dnf", "pacman", "zypper", "emerge", "brew", "winget", "choco", "scoop", "snap", "flatpak", "bun", "npm"}
	for _, m := range all {
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

func confirmActionGo(assumeYes bool, prompt string) bool {
	if assumeYes || assumeYesEnvGo() {
		return true
	}
	fmt.Fprintf(os.Stderr, "%s [Y/n]: ", prompt)
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "" || line == "y" || line == "yes"
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
