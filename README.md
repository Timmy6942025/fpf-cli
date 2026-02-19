# Fuzzy Package Finder (`fpf`)

`fpf` is a cross-platform fuzzy package finder powered by `fzf`.

It auto-detects your OS and distro package manager, then lets you fuzzy-search packages, preview details, and install/remove/update with one interface.

## Supported package managers

- Linux distro managers: `apt`, `dnf`, `pacman`, `zypper`, `emerge`
- Cross-platform app managers: `snap`, `flatpak`
- Dev package managers: `npm`, `bun`
- macOS: `brew`

`winget` is intentionally not supported.

## Features

- Auto-detects default manager from OS + distro
- Supports manager override with one or two-letter flags
- Unified fuzzy UI for search/install/list/remove
- Multi-select installs/removals with safety confirmation
- Shared keybinds and preview pane behavior across managers

## Requirements

- `bash`
- `fzf`
- One or more supported package managers from the list above

## Usage

```bash
fpf [manager option] [action option] [query]
fpf -m|--manager <name> [action option] [query]
```

Default action is `search + install` using auto-detected manager.

## Install

```bash
# npm
npm install -g fuzzy-pkg-finder-ultimate

# bun
bun add -g fuzzy-pkg-finder-ultimate
```

### Action options

- `-l`, `--list-installed` - fuzzy list installed packages and inspect details
- `-R`, `--remove` - fuzzy select installed packages to remove
- `-U`, `--update` - run manager update/upgrade flow
- `-h`, `--help` - show help

### Manager options

- `-ap`, `--apt`
- `-dn`, `--dnf`
- `-pm`, `--pacman`
- `-zy`, `--zypper`
- `-em`, `--emerge`
- `-br`, `--brew`
- `-sn`, `--snap`
- `-fp`, `--flatpak`
- `-np`, `--npm`
- `-bn`, `--bun`
- `-ad`, `--auto` (force auto-detection)
- `-m`, `--manager <name>` (explicit manager by name)

### Examples

```bash
# Auto-detected manager search/install
fpf ripgrep

# Force DNF
fpf -dn nginx

# Use APT and list installed packages
fpf -ap -l openssl

# Remove with Homebrew
fpf -br -R wget

# Update with auto-detected manager
fpf -U

# Explicit manager name
fpf --manager flatpak gimp
```

## Keybinds

- `ctrl-h` show help in preview
- `ctrl-k` show keybinds in preview
- `ctrl-/` toggle preview
- `ctrl-n` jump to next selected item
- `ctrl-b` jump to previous selected item

## Notes

- Root-required managers (`apt`, `dnf`, `pacman`, `zypper`, `emerge`, `snap`) run privileged operations via `sudo` when needed.
- User-space managers (`brew`, `npm`, `bun`, and default user-mode `flatpak`) run without `sudo`.
