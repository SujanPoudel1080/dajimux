#!/usr/bin/env bash
set -euo pipefail

VERSION="${1:-0.1.0}"
COMMIT="$(git rev-parse --short HEAD)"
BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
DIST_DIR="dist"

echo "Building dajimux v${VERSION} (commit: ${COMMIT}, date: ${BUILD_DATE})..."
rm -rf "${DIST_DIR}"
mkdir -p "${DIST_DIR}"

TARGETS=(
    "darwin arm64"
    "darwin amd64"
    "linux amd64"
    "linux arm64"
)

LDFLAGS="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${BUILD_DATE}"

for TARGET in "${TARGETS[@]}"; do
    OS="$(echo $TARGET | cut -d' ' -f1)"
    ARCH="$(echo $TARGET | cut -d' ' -f2)"
    OUTPUT_BIN="${DIST_DIR}/dajimux_${OS}_${ARCH}"
    
    echo "  -> Compiling ${OS}/${ARCH}..."
    CGO_ENABLED=0 GOOS="${OS}" GOARCH="${ARCH}" go build -ldflags "${LDFLAGS}" -o "${OUTPUT_BIN}" ./cmd/dajimux
    
    # Archive
    ARCHIVE_DIR="${DIST_DIR}/dajimux_${VERSION}_${OS}_${ARCH}"
    mkdir -p "${ARCHIVE_DIR}"
    cp "${OUTPUT_BIN}" "${ARCHIVE_DIR}/dajimux"
    cp README.md LICENSE "${ARCHIVE_DIR}/"
    
    TARBALL="${DIST_DIR}/dajimux_${VERSION}_${OS}_${ARCH}.tar.gz"
    tar -czf "${TARBALL}" -C "${ARCHIVE_DIR}" dajimux README.md LICENSE
    rm -rf "${ARCHIVE_DIR}" "${OUTPUT_BIN}"
    echo "     Created ${TARBALL}"
done

echo "Generating SHA256 checksums..."
(
    cd "${DIST_DIR}"
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum dajimux_*.tar.gz > checksums.txt
    elif command -v shasum >/dev/null 2>&1; then
        shasum -a 256 dajimux_*.tar.gz > checksums.txt
    fi
)

echo "Release build complete! Assets in ${DIST_DIR}/:"
ls -lh "${DIST_DIR}"
