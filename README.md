# fpf-cli (`fpf`) 📦

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

# Refresh package catalogs only
fpf --refresh
```

By default, `fpf` auto-detects your package manager.

On every OS, default auto mode includes all supported detected managers. If both `bun` and `npm` are available, auto mode keeps `bun` and skips `npm`.

For no-query startup (`fpf`), each manager uses a lighter default query and per-manager result cap to keep startup responsive.

Live reload is enabled by default, with a minimum query length and debounce to reduce lag while typing.

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
- `--refresh` refresh package catalogs only
- `-y, --yes` skip confirmation prompts
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
- If `fzf` is missing, `fpf` auto-installs it using a compatible detected manager, then falls back to a release binary download if manager bootstrap fails.
- Root managers (`apt`, `dnf`, `pacman`, `zypper`, `emerge`, `snap`) use `sudo` when needed.
- If Flatpak is detected and Flathub is missing, `fpf` attempts `flatpak remote-add --if-not-exists --user flathub ...` automatically.
- Set `FPF_ASSUME_YES=1` to bypass confirmation prompts in non-interactive flows.
- `FPF_DYNAMIC_RELOAD`: `always` (default), `single`, or `never`
- Live reload uses `change:reload` by default for reliability (`ctrl-r` uses the same reload command).
- Set `FPF_DYNAMIC_RELOAD_TRANSPORT=ipc` to opt into `--listen` + IPC query notifications on supported `fzf` builds.
- `FPF_RELOAD_MIN_CHARS`: minimum query length before live reload (default `2`)
- `FPF_RELOAD_DEBOUNCE`: reload debounce seconds (default `0.12`)
- `FPF_DISABLE_INSTALLED_CACHE=1` disables installed-package marker cache
- `FPF_INSTALLED_CACHE_TTL`: installed-package marker cache freshness window in seconds (default `300`, set `0` to always refresh)
