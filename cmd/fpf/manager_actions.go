package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

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
	if err := validateManagerName(input.Manager); err != nil {
		return input, true, err
	}
	validActions := map[string]bool{"install": true, "remove": true, "show_info": true, "update": true, "refresh": true}
	if !validActions[input.Action] {
		return input, true, fmt.Errorf("unsupported action: %q", input.Action)
	}
	if len(input.Packages) > 100 {
		return input, true, fmt.Errorf("too many packages: %d > 100", len(input.Packages))
	}

	return input, true, nil
}

var pkgNameRegex = regexp.MustCompile(`^[a-zA-Z0-9._\-/@+]+$`)

func isValidPkgName(pkg string) bool {
	if pkg == "" {
		return false
	}
	if len(pkg) > maxPackageLength {
		return false
	}
	if strings.HasPrefix(pkg, "-") {
		return false
	}
	if strings.Contains(pkg, "\x00") || strings.Contains(pkg, "\n") || strings.Contains(pkg, "\r") {
		return false
	}
	return pkgNameRegex.MatchString(pkg)
}

func validatePkgs(pkgs []string) error {
	if len(pkgs) > 100 {
		return fmt.Errorf("too many packages: %d > 100", len(pkgs))
	}
	for _, pkg := range pkgs {
		if !isValidPkgName(pkg) {
			return fmt.Errorf("invalid package name %q: must match ^[a-zA-Z0-9._\\-/@+]+$ and not start with '-', length <= %d", pkg, maxPackageLength)
		}
	}
	return nil
}

func executeManagerAction(input managerActionInput) error {
	manager := input.Manager
	action := input.Action
	pkgs := input.Packages
	if err := validateManagerName(manager); err != nil {
		return err
	}
	if len(pkgs) > 0 {
		if err := validatePkgs(pkgs); err != nil {
			return err
		}
	}
	if len(manager) == 0 || len(action) == 0 {
		return fmt.Errorf("manager and action required")
	}
	if action == "show_info" && len(pkgs) == 0 {
		return fmt.Errorf("show_info requires a package name")
	}

	switch manager {
	case "apt":
		switch action {
		case "install":
			return runRootCommand("apt-get", append([]string{"install", "-y", "--"}, pkgs...)...)
		case "remove":
			return runRootCommand("apt-get", append([]string{"remove", "-y", "--"}, pkgs...)...)
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
			return runRootCommand("dnf", append([]string{"install", "-y", "--"}, pkgs...)...)
		case "remove":
			return runRootCommand("dnf", append([]string{"remove", "-y", "--"}, pkgs...)...)
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
			return runRootCommand("pacman", append([]string{"-S", "--needed", "--"}, pkgs...)...)
		case "remove":
			return runRootCommand("pacman", append([]string{"-Rsn", "--"}, pkgs...)...)
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
			return runRootCommand("zypper", append([]string{"--non-interactive", "install", "--auto-agree-with-licenses", "--"}, pkgs...)...)
		case "remove":
			return runRootCommand("zypper", append([]string{"--non-interactive", "remove", "--"}, pkgs...)...)
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
			return runCommand("brew", append([]string{"install", "--"}, pkgs...)...)
		case "remove":
			return runCommand("brew", append([]string{"uninstall", "--"}, pkgs...)...)
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
			pkg := firstPackage(pkgs)
			if pkg == "" {
				return fmt.Errorf("package name required for show_info")
			}
			return runCommandQuietErr("choco", "info", pkg)
		case "update":
			return runCommand("choco", "upgrade", "all", "-y")
		case "refresh":
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "choco", "source", "list", "--limit-output")
			cmd.Env = os.Environ()
			cmd.Stdin = os.Stdin
			cmd.Stdout = io.Discard
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil && ctx.Err() == context.DeadlineExceeded {
				return context.DeadlineExceeded
			} else {
				return err
			}
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
			return runCommand("npm", append([]string{"install", "-g", "--"}, pkgs...)...)
		case "remove":
			return runCommand("npm", append([]string{"uninstall", "-g", "--"}, pkgs...)...)
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
			return runCommand("bun", append([]string{"add", "-g", "--"}, pkgs...)...)
		case "remove":
			return runCommand("bun", append([]string{"remove", "--global", "--"}, pkgs...)...)
		case "show_info":
			pkg := firstPackage(pkgs)
			if pkg == "" {
				return fmt.Errorf("package name required for show_info")
			}
			if err := runCommandQuietErr("bun", "info", pkg); err != nil {
				return runCommandQuietErr("npm", "view", pkg)
			}
			return nil
		case "update":
			return runCommand("bun", "update", "--global")
		case "refresh":
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "bun", "pm", "cache")
			cmd.Env = os.Environ()
			cmd.Stdin = os.Stdin
			cmd.Stdout = io.Discard
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil && ctx.Err() == context.DeadlineExceeded {
				return context.DeadlineExceeded
			} else {
				return err
			}
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
	if name == "" {
		return fmt.Errorf("empty command name")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = os.Environ()
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("%s timed out after 5m: %w", name, context.DeadlineExceeded)
		}
		return err
	}
	return nil
}

func runCommandQuietErr(name string, args ...string) error {
	if name == "" {
		return fmt.Errorf("empty command name")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = os.Environ()
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return context.DeadlineExceeded
		}
		return err
	}
	return nil
}

func cleanEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if strings.HasPrefix(kv, "FPF_TEST_") {
			// keep test-only vars so mocks in tests can function
			out = append(out, kv)
			continue
		}
		if strings.HasPrefix(kv, "FPF_") {
			continue
		}
		out = append(out, kv)
	}
	return out
}

func runRootCommand(name string, args ...string) error {
	if name == "" {
		return fmt.Errorf("empty command name")
	}
	if needsRoot(name) && os.Geteuid() != 0 {
		if _, err := exec.LookPath("sudo"); err != nil {
			return errors.New("requires root privileges and sudo was not found")
		}
		sudoArgs := append([]string{"-n", name}, args...)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, "sudo", sudoArgs...)
		cmd.Env = cleanEnv(os.Environ())
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				return fmt.Errorf("sudo %s timed out: %w", name, context.DeadlineExceeded)
			}
			return err
		}
		return nil
	}
	return runCommand(name, args...)
}

func runRootCommandQuietErr(name string, args ...string) error {
	if name == "" {
		return fmt.Errorf("empty command name")
	}
	if needsRoot(name) && os.Geteuid() != 0 {
		if _, err := exec.LookPath("sudo"); err != nil {
			return errors.New("requires root privileges and sudo was not found")
		}
		sudoArgs := append([]string{"-n", name}, args...)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "sudo", sudoArgs...)
		cmd.Env = cleanEnv(os.Environ())
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = io.Discard
		if err := cmd.Run(); err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				return context.DeadlineExceeded
			}
			return err
		}
		return nil
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
