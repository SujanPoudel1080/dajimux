# dajimux Release & Distribution Guide

This document outlines the multi-platform release strategy and distribution infrastructure for **dajimux** on **macOS** and **Linux**.

---

## Supported Architectures & Platforms

All binaries are statically compiled with `CGO_ENABLED=0` to ensure zero runtime dynamic linker dependencies (compatible with Ubuntu, Debian, Alpine, Fedora, Arch, and macOS):

| Platform | Architecture | Binary / Package Target |
| :--- | :--- | :--- |
| **macOS (Apple Silicon)** | `arm64` (M1/M2/M3/M4) | `dajimux_darwin_arm64.tar.gz`, Homebrew |
| **macOS (Intel)** | `amd64` (x86_64) | `dajimux_darwin_amd64.tar.gz`, Homebrew |
| **Linux (x86_64)** | `amd64` | `dajimux_linux_amd64.tar.gz`, `.deb`, `.rpm` |
| **Linux (AArch64)** | `arm64` | `dajimux_linux_arm64.tar.gz`, `.deb`, `.rpm` |

---

## Distribution Channels

### 1. One-Line Shell Installer (`curl | bash`)
Users on any macOS or Linux machine can run:
```bash
curl -fsSL https://raw.githubusercontent.com/SujanPoudel1080/dajimux/main/install.sh | bash
```
The script:
1. Detects OS (`darwin` / `linux`) and architecture (`amd64` / `arm64`).
2. Queries GitHub Releases API for the latest version.
3. Downloads the archive and verifies the SHA256 checksum.
4. Installs the binary to `/usr/local/bin` (or `~/.local/bin` if rootless).
5. Validates installation with `dajimux version`.

---

### 2. Homebrew Tap (macOS & Linuxbrew)
Create a public repository named `homebrew-tap` under your GitHub account (`github.com/SujanPoudel1080/homebrew-tap`).

Users install via:
```bash
brew tap SujanPoudel1080/tap
brew install dajimux
```

GoReleaser automatically generates and pushes the formula to `Formula/dajimux.rb` in your tap repository on each release.

---

### 3. Linux Package Managers (`.deb` & `.rpm`)
GoReleaser packages Debian (`.deb`) and Red Hat / Fedora (`.rpm`) packages via built-in `nfpm`.

- **Debian / Ubuntu / Pop!_OS / Linux Mint**:
  ```bash
  sudo dpkg -i dajimux_0.1.0_linux_amd64.deb
  ```
- **Fedora / RHEL / Rocky Linux / openSUSE**:
  ```bash
  sudo rpm -i dajimux_0.1.0_linux_amd64.rpm
  ```

---

### 4. Go Toolchain
```bash
go install github.com/SujanPoudel1080/dajimux/cmd/dajimux@latest
```

---

### 5. Arch Linux (AUR)
Create an AUR package `dajimux-bin` using the prebuilt release binaries:

```bash
# PKGBUILD snippet
pkgname=dajimux-bin
pkgver=0.1.0
pkgrel=1
pkgdesc="Modern terminal multiplexer and window manager with Vim ergonomics"
arch=('x86_64' 'aarch64')
url="https://github.com/SujanPoudel1080/dajimux"
license=('MIT')
source_x86_64=("https://github.com/SujanPoudel1080/dajimux/releases/download/v${pkgver}/dajimux_${pkgver}_linux_amd64.tar.gz")
source_aarch64=("https://github.com/SujanPoudel1080/dajimux/releases/download/v${pkgver}/dajimux_${pkgver}_linux_arm64.tar.gz")

package() {
    install -Dm755 "${srcdir}/dajimux" "${pkgdir}/usr/bin/dajimux"
}
```

---

## Step-by-Step: Publishing a New Release

### Step 1: Ensure Working Tree is Clean
```bash
git status
go test ./...
```

### Step 2: Create a Git Release Tag
Follow [Semantic Versioning](https://semver.org/) (`vMAJOR.MINOR.PATCH`):

```bash
git tag -a v0.1.0 -m "Release v0.1.0: Initial public release"
```

### Step 3: Push the Tag to GitHub
```bash
git push origin v0.1.0
```

### Step 4: Automated CI/CD
GitHub Actions (`.github/workflows/release.yml`) activates automatically:
1. Checks out repository at tag `v0.1.0`.
2. Compiles binaries with injected build flags (`-X main.version=0.1.0 -X main.commit=<sha> -X main.date=<utc>`).
3. Bundles tarballs for all 4 OS/arch pairs.
4. Generates `.deb` and `.rpm` Linux packages.
5. Generates `checksums.txt`.
6. Creates the GitHub Release with automatic changelog.
7. Updates the Homebrew Tap formula.

---

## Setting up Homebrew Tap (One-Time Setup)

To allow `brew install SujanPoudel1080/tap/dajimux`:
1. Go to [GitHub New Repository](https://github.com/new).
2. Repository name: `homebrew-tap`.
3. Set to **Public**, initialize with a README.
4. Create a Personal Access Token (PAT) with `repo` scope at [GitHub Tokens](https://github.com/settings/tokens).
5. In `dajimux` repository settings -> **Secrets and variables** -> **Actions**:
   - Add secret name: `TAP_GITHUB_TOKEN`
   - Secret value: `<your personal access token>`
