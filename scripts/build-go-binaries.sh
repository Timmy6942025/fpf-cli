#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

cd "${ROOT_DIR}"

if ! command -v go >/dev/null 2>&1; then
    echo "go toolchain not found; cannot build packaged binaries" >&2
    exit 1
fi

mkdir -p "${ROOT_DIR}/bin"

targets=(
    "linux/amd64"
    "linux/arm64"
    "darwin/amd64"
    "darwin/arm64"
    "windows/amd64"
    "windows/arm64"
)

for target in "${targets[@]}"; do
    IFS=/ read -r goos goarch <<<"${target}"

    output_path="${ROOT_DIR}/bin/fpf-go-${goos}-${goarch}"
    if [[ "${goos}" == "windows" ]]; then
        output_path="${output_path}.exe"
    fi

    echo "Building ${goos}/${goarch} -> ${output_path}"
    CGO_ENABLED=0 GOOS="${goos}" GOARCH="${goarch}" go build -trimpath -ldflags="-s -w" -o "${output_path}" ./cmd/fpf
done
