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

On macOS, default auto mode uses both `brew` and `bun` together.

On Linux, default auto mode uses your distro manager plus installed cross-platform managers (`snap`, `flatpak`, `brew`, `npm`, `bun`).

## Supported Managers

- Linux: `apt`, `dnf`, `pacman`, `zypper`, `emerge`
- Cross-platform: `snap`, `flatpak`
- Dev: `npm`, `bun`
- macOS: `brew`

(`winget` intentionally not included.)

## Manager Override Flags

- `-ap` apt
- `-dn` dnf
- `-pm` pacman
- `-zy` zypper
- `-em` emerge
- `-br` brew
- `-sn` snap
- `-fp` flatpak
- `-np` npm
- `-bn` bun
- `-m, --manager <name>` full manager name

## Common Options

- `-l, --list-installed` list installed packages
- `-R, --remove` remove selected packages
- `-U, --update` run update/upgrade flow
- `-h, --help` show help

## Keybinds

- `ctrl-h` help in preview
- `ctrl-k` keybinds in preview
- `ctrl-/` toggle preview
- `ctrl-n` next selected item
- `ctrl-b` previous selected item

## Notes

- Requires: `bash` + `fzf`
- Root managers (`apt`, `dnf`, `pacman`, `zypper`, `emerge`, `snap`) use `sudo` when needed.
