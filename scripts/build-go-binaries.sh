#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

cd "${ROOT_DIR}"

if ! command -v go >/dev/null 2>&1; then
    echo "go toolchain not found; cannot build packaged binaries" >&2
    exit 1
fi

# Verify code before building
echo "Running go vet..."
go vet ./...
if [ -n "$(gofmt -l .)" ]; then
    echo "gofmt check failed for:"
    gofmt -l .
    exit 1
fi

mkdir -p "${ROOT_DIR}/bin"

# Derive version from package.json for ldflags
VERSION="$(node -p "require('./package.json').version" 2>/dev/null || echo "dev")"
LDFLAGS="-s -w -X main.version=${VERSION}"

targets=(
    "linux/amd64"
    "linux/arm64"
    "darwin/amd64"
    "darwin/arm64"
    "windows/amd64"
    "windows/arm64"
)

pids=()
for target in "${targets[@]}"; do
    IFS=/ read -r goos goarch <<<"${target}"

    output_path="${ROOT_DIR}/bin/fpf-go-${goos}-${goarch}"
    if [[ "${goos}" == "windows" ]]; then
        output_path="${output_path}.exe"
    fi

    echo "Building ${goos}/${goarch} -> ${output_path} (version ${VERSION})"
    CGO_ENABLED=0 GOOS="${goos}" GOARCH="${goarch}" go build -trimpath -ldflags="${LDFLAGS}" -o "${output_path}" ./cmd/fpf &
    pids+=($!)
done

# Wait for all parallel builds
for pid in "${pids[@]}"; do
    wait "$pid"
done

# Generate SHA256 checksums
echo "Generating SHA256 checksums..."
(
    cd "${ROOT_DIR}/bin"
    sha256sum fpf-go-* 2>/dev/null | tee SHA256SUMS || shasum -a 256 fpf-go-* 2>/dev/null | tee SHA256SUMS
)
echo "Build complete. Binaries in ${ROOT_DIR}/bin"
