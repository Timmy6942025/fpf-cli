manager_execute_action() {
    local manager="$1"
    local action="$2"
    shift 2
    local package=""
    local pkg=""

    if [[ "${FPF_USE_GO_MANAGER_ACTIONS:-0}" == "1" && -n "${FPF_SELF_PATH:-}" && -x "${FPF_SELF_PATH}" ]]; then
        "${FPF_SELF_PATH}" --go-manager-action "${action}" --go-manager "${manager}" -- "$@"
        return $?
    fi

    case "${manager}" in
        apt)
            case "${action}" in
                install)
                    run_as_root apt-get install -y "$@"
                    ;;
                remove)
                    run_as_root apt-get remove -y "$@"
                    ;;
                show_info)
                    package="${1:-}"
                    cat <(apt-cache show "${package}" 2>/dev/null) <(printf "\n") <(dpkg -L "${package}" 2>/dev/null)
                    ;;
                update)
                    run_as_root apt-get update
                    run_as_root apt-get upgrade -y
                    ;;
                refresh)
                    run_as_root apt-get update
                    ;;
                *)
                    return 1
                    ;;
            esac
            ;;
        dnf)
            case "${action}" in
                install)
                    run_as_root dnf install -y "$@"
                    ;;
                remove)
                    run_as_root dnf remove -y "$@"
                    ;;
                show_info)
                    package="${1:-}"
                    cat <(dnf info "${package}" 2>/dev/null) <(printf "\n") <(rpm -ql "${package}" 2>/dev/null)
                    ;;
                update)
                    run_as_root dnf upgrade -y
                    ;;
                refresh)
                    run_as_root dnf makecache
                    ;;
                *)
                    return 1
                    ;;
            esac
            ;;
        pacman)
            case "${action}" in
                install)
                    run_as_root pacman -S --needed "$@"
                    ;;
                remove)
                    run_as_root pacman -Rsn "$@"
                    ;;
                show_info)
                    package="${1:-}"
                    cat <(pacman -Qi "${package}" 2>/dev/null || pacman -Si "${package}" 2>/dev/null) <(printf "\n") <(pacman -Ql "${package}" 2>/dev/null)
                    ;;
                update)
                    run_as_root pacman -Syu
                    ;;
                refresh)
                    run_as_root pacman -Sy
                    ;;
                *)
                    return 1
                    ;;
            esac
            ;;
        zypper)
            case "${action}" in
                install)
                    run_as_root zypper --non-interactive install --auto-agree-with-licenses "$@"
                    ;;
                remove)
                    run_as_root zypper --non-interactive remove "$@"
                    ;;
                show_info)
                    package="${1:-}"
                    zypper --non-interactive info "${package}" 2>/dev/null
                    ;;
                update)
                    run_as_root zypper --non-interactive refresh
                    run_as_root zypper --non-interactive update
                    ;;
                refresh)
                    run_as_root zypper --non-interactive refresh
                    ;;
                *)
                    return 1
                    ;;
            esac
            ;;
        emerge)
            case "${action}" in
                install)
                    run_as_root emerge --ask=n --verbose "$@"
                    ;;
                remove)
                    run_as_root emerge --ask=n --deselect "$@"
                    run_as_root emerge --ask=n --depclean "$@"
                    ;;
                show_info)
                    package="${1:-}"
                    emerge --search --color=n "${package}" 2>/dev/null
                    ;;
                update)
                    run_as_root emerge --sync
                    run_as_root emerge --ask=n --update --deep --newuse @world
                    ;;
                refresh)
                    run_as_root emerge --sync
                    ;;
                *)
                    return 1
                    ;;
            esac
            ;;
        brew)
            case "${action}" in
                install)
                    brew install "$@"
                    ;;
                remove)
                    brew uninstall "$@"
                    ;;
                show_info)
                    package="${1:-}"
                    brew info "${package}" 2>/dev/null
                    ;;
                update)
                    brew update
                    brew upgrade
                    ;;
                refresh)
                    brew update
                    ;;
                *)
                    return 1
                    ;;
            esac
            ;;
        winget)
            case "${action}" in
                install)
                    for pkg in "$@"; do
                        winget install --id "${pkg}" --exact --source winget --accept-package-agreements --accept-source-agreements --disable-interactivity
                    done
                    ;;
                remove)
                    for pkg in "$@"; do
                        winget uninstall --id "${pkg}" --exact --source winget --disable-interactivity
                    done
                    ;;
                show_info)
                    package="${1:-}"
                    winget show --id "${package}" --exact --source winget --accept-source-agreements --disable-interactivity 2>/dev/null
                    ;;
                update)
                    winget upgrade --all --source winget --accept-package-agreements --accept-source-agreements --disable-interactivity
                    ;;
                refresh)
                    winget source update --name winget --accept-source-agreements --disable-interactivity
                    ;;
                *)
                    return 1
                    ;;
            esac
            ;;
        choco)
            case "${action}" in
                install)
                    choco install "$@" -y
                    ;;
                remove)
                    choco uninstall "$@" -y
                    ;;
                show_info)
                    package="${1:-}"
                    choco info "${package}" 2>/dev/null
                    ;;
                update)
                    choco upgrade all -y
                    ;;
                refresh)
                    choco source list --limit-output >/dev/null
                    ;;
                *)
                    return 1
                    ;;
            esac
            ;;
        scoop)
            case "${action}" in
                install)
                    scoop install "$@"
                    ;;
                remove)
                    scoop uninstall "$@"
                    ;;
                show_info)
                    package="${1:-}"
                    scoop info "${package}" 2>/dev/null
                    ;;
                update)
                    scoop update
                    scoop update "*"
                    ;;
                refresh)
                    scoop update
                    ;;
                *)
                    return 1
                    ;;
            esac
            ;;
        snap)
            case "${action}" in
                install)
                    for pkg in "$@"; do
                        run_as_root snap install "${pkg}" 2>/dev/null || run_as_root snap install --classic "${pkg}"
                    done
                    ;;
                remove)
                    run_as_root snap remove "$@"
                    ;;
                show_info)
                    package="${1:-}"
                    snap info "${package}" 2>/dev/null
                    ;;
                update)
                    run_as_root snap refresh
                    ;;
                refresh)
                    run_as_root snap refresh --list
                    ;;
                *)
                    return 1
                    ;;
            esac
            ;;
        flatpak)
            case "${action}" in
                install)
                    for pkg in "$@"; do
                        flatpak install -y --user flathub "${pkg}" 2>/dev/null ||
                            flatpak install -y --user "${pkg}" 2>/dev/null ||
                            run_as_root flatpak install -y flathub "${pkg}" 2>/dev/null ||
                            run_as_root flatpak install -y "${pkg}"
                    done
                    ;;
                remove)
                    flatpak uninstall -y --user "$@" 2>/dev/null || run_as_root flatpak uninstall -y "$@"
                    ;;
                show_info)
                    package="${1:-}"
                    flatpak info "${package}" 2>/dev/null || flatpak remote-info flathub "${package}" 2>/dev/null
                    ;;
                update)
                    flatpak update -y --user 2>/dev/null || run_as_root flatpak update -y
                    ;;
                refresh)
                    ensure_flatpak_flathub_remote >/dev/null 2>&1 || true
                    flatpak update -y --appstream --user 2>/dev/null || run_as_root flatpak update -y --appstream
                    ;;
                *)
                    return 1
                    ;;
            esac
            ;;
        npm)
            case "${action}" in
                install)
                    npm install -g "$@"
                    ;;
                remove)
                    npm uninstall -g "$@"
                    ;;
                show_info)
                    package="${1:-}"
                    npm view "${package}" 2>/dev/null
                    ;;
                update)
                    npm update -g
                    ;;
                refresh)
                    npm cache verify
                    ;;
                *)
                    return 1
                    ;;
            esac
            ;;
        bun)
            case "${action}" in
                install)
                    bun add -g "$@"
                    ;;
                remove)
                    bun remove --global "$@"
                    ;;
                show_info)
                    package="${1:-}"
                    bun info "${package}" 2>/dev/null || npm view "${package}" 2>/dev/null
                    ;;
                update)
                    bun update --global
                    ;;
                refresh)
                    bun pm cache >/dev/null
                    ;;
                *)
                    return 1
                    ;;
            esac
            ;;
        *)
            return 1
            ;;
    esac
}
