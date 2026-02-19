# fpf-cli (`fpf`)

Simple fuzzy package finder for people who live in the terminal.

Search packages with `fzf`, preview details, and install/remove/update from one place.

![Screenshot](./fpf.png)

## Install

```bash
# npm
npm install -g fpf-cli

# bun
bun add -g fpf-cli
```

## Quick Start

```bash
# Search + install (default action)
fpf ripgrep

# List installed packages
fpf -l

# Remove packages
fpf -R

# Update packages
fpf -U
```

By default, `fpf` auto-detects your package manager.

On every OS, default auto mode now includes all supported package managers that are detected on your system in one combined list.

For no-query startup (`fpf`), each manager uses a lighter default query and per-manager result cap to keep startup responsive.

Live reload is enabled by default for single-manager mode (`--manager`), and disabled by default in all-manager mode to avoid lag/flicker while navigating.

## Supported Managers

- Linux: `apt`, `dnf`, `pacman`, `zypper`, `emerge`
- Windows: `winget`, `choco`, `scoop`
- Cross-platform: `snap`, `flatpak`
- Dev: `npm`, `bun`
- macOS: `brew`

## Manager Override Flags

- `-ap` apt
- `-dn` dnf
- `-pm` pacman
- `-zy` zypper
- `-em` emerge
- `-br` brew
- `-wg` winget
- `-ch` choco
- `-sc` scoop
- `-sn` snap
- `-fp` flatpak
- `-np` npm
- `-bn` bun
- `-m, --manager <name>` full manager name

## Common Options

- `-l, --list-installed` list installed packages
- `-R, --remove` remove selected packages
- `-U, --update` run update/upgrade flow
- `-v, --version` print version and exit
- `-h, --help` show help

## Keybinds

- `ctrl-h` help in preview
- `ctrl-k` keybinds in preview
- `ctrl-/` toggle preview
- `ctrl-n` next selected item
- `ctrl-b` previous selected item

Installed packages are marked with `*` in the result list.

## Notes

- Requires: `bash` + `fzf`
- If `fzf` is missing, `fpf` auto-installs it using a compatible detected manager.
- Root managers (`apt`, `dnf`, `pacman`, `zypper`, `emerge`, `snap`) use `sudo` when needed.
- `FPF_DYNAMIC_RELOAD`: `single` (default), `always`, or `never`
- `FPF_DISABLE_INSTALLED_CACHE=1` disables installed-package marker cache
