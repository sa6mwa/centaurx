# Centaurx Runner Image License Audit

Date: 2026-01-10
Scope: `bootstrap/templates/Containerfile.cxrunner.tmpl` + `bootstrap/files/cxrunner-install.sh` (runner image build).

## Inventory (what the runner image installs)

### Base image
- `debian:stable-slim`

### APT packages (from Debian repos)
The install script uses Debian APT to install (plus a couple of optional fallbacks):
- `apt-transport-https`, `bash`, `build-essential`, `ca-certificates`, `clang`, `cmake`, `chromium`, `chromium-sandbox`, `fontconfig`,
  `fonts-liberation`, `fonts-noto-color-emoji`, `fonts-noto-core`, `curl`, `dirmngr`, `fd-find`, `fzf`, `gdb`, `git`, `git-lfs`,
  `gnupg`, `gnuplot-nox`, `groovy`, `jq`, `iproute2`, `lldb`, `lld`, `libbz2-dev`, `libcurl4-openssl-dev`, `libffi-dev`,
  `liblzma-dev`, `libreadline-dev`, `libsqlite3-dev`, `libssl-dev`, `libxml2-dev`, `libstdc++6`, `libtinfo6`, `lsof`, `maven`,
  `ninja-build`, `dnsutils`, `openssh-client`, `pkg-config`, `python3`, `python3-dev`, `python3-pip`, `python3-setuptools`, `python3-venv`,
  `python3-wheel`, `pipx`, `r-base`, `r-base-dev`, `ripgrep`, `strace`, `tree`, `unzip`, `xz-utils`, `zip`, `zlib1g-dev`
- Optional: `libz1` (if available), and `libncurses6` else `libncurses5`.

Note: Debian packages come from Debian’s default repositories. Individual licenses are not enumerated here; Debian main packages are generally redistributable under their respective open-source licenses. (If needed, this can be expanded into per-package licenses.)

### Non-APT installs (downloaded or built during image build)
- OpenJDK (via APT): `default-jdk-headless` or `openjdk-21-jdk-headless`
- Node.js (via NodeSource apt repo + `nodejs` package)
- Bun (downloaded via `https://bun.sh/install`)
- Go (downloaded from `https://go.dev/dl/...`)
- Rust (via `rustup` from `https://sh.rustup.rs`)
- .NET SDK (via Microsoft apt repo `packages.microsoft.com`)
- Gradle (downloaded from `https://services.gradle.org/distributions/...`)
- Android SDK command-line tools + platform/build tools (downloaded from `https://dl.google.com/android/repository/...`)
- Gru (go install of `pkt.systems/gruno/cmd/gru@latest`)
- vcpkg (git clone from `https://github.com/microsoft/vcpkg`)
- Conan (installed via `pipx install conan`)
- Codex CLI (downloaded from `https://github.com/openai/codex` releases)

## Redistributable image contents (current Docker Hub release)
This section enumerates the software included in the redistributable runner image.

### Base image
- `debian:stable-slim`

### APT packages
- `apt-transport-https`, `bash`, `build-essential`, `ca-certificates`, `clang`, `cmake`, `chromium`, `chromium-sandbox`, `fontconfig`,
  `fonts-liberation`, `fonts-noto-color-emoji`, `fonts-noto-core`, `curl`, `dirmngr`, `fd-find`, `fzf`, `gdb`, `git`, `git-lfs`,
  `gnupg`, `gnuplot-nox`, `groovy`, `jq`, `iproute2`, `lldb`, `lld`, `libbz2-dev`, `libcurl4-openssl-dev`, `libffi-dev`,
  `liblzma-dev`, `libreadline-dev`, `libsqlite3-dev`, `libssl-dev`, `libxml2-dev`, `libstdc++6`, `libtinfo6`, `lsof`, `maven`,
  `ninja-build`, `dnsutils`, `openssh-client`, `pkg-config`, `python3`, `python3-dev`, `python3-pip`, `python3-setuptools`, `python3-venv`,
  `python3-wheel`, `pipx`, `r-base`, `r-base-dev`, `ripgrep`, `strace`, `tree`, `unzip`, `xz-utils`, `zip`, `zlib1g-dev`
- Optional: `libz1` (if available), and `libncurses6` else `libncurses5`.

### Additional tooling (non-APT)
- OpenJDK (via APT): `default-jdk-headless` or `openjdk-21-jdk-headless`
- Node.js (via NodeSource apt repo + `nodejs` package)
- Go (downloaded from `https://go.dev/dl/...`)
- Rust (via `rustup` from `https://sh.rustup.rs`)
- .NET SDK (via Microsoft apt repo `packages.microsoft.com`) — license and third-party notices copied into `/usr/share/licenses/centaurxrunner/dotnet/`
- Gradle (downloaded from `https://services.gradle.org/distributions/...`)
- Gru (go install of `pkt.systems/gruno/cmd/gru@latest`)
- vcpkg (git clone from `https://github.com/microsoft/vcpkg`)
- Conan (installed via `pipx install conan`)
- Codex CLI (downloaded from `https://github.com/openai/codex` releases)

## Full (non-redistributable) runner image contents
The full image includes everything listed above **plus** the following additional components:
- Android SDK command-line tools, platform-tools, build-tools, and platform APIs.
- Bun (installed via `https://bun.sh/install`).

## License analysis for non-APT components

| Component | Source | License (summary) | Redistributable? | Notes / obligations | Action for redistributable image |
| --- | --- | --- | --- | --- | --- |
| Android SDK | developer.android.com / Google | Android SDK terms restrict redistribution | **No** | License terms do not allow redistribution of the SDK without separate rights | **Exclude** |
| Bun | bun.sh | MIT for Bun, but statically links LGPL-2 components (JavaScriptCore/WebKit) | **Yes, but conditional** | LGPL static-linking requires providing relinkable object files or equivalent compliance; current build does not provide those | **Exclude (no LGPL compliance artifacts today)** |
| Go | go.dev | BSD-style license | **Yes** | Retain license/notice | Include |
| Rust toolchain | rust-lang.org | Dual MIT/Apache-2.0 | **Yes** | Retain license/notice | Include |
| rustup | rust-lang/rustup | Dual MIT/Apache-2.0 | **Yes** | Retain license/notice | Include |
| Node.js | nodejs.org | MIT license | **Yes** | Node bundles third‑party components; retain notices | Include |
| .NET SDK | .NET license information + Microsoft .NET Library License | Linux/macOS distributions use MIT; Windows uses .NET Library License | **Yes on Linux/macOS** | Preserve license/third‑party notices; avoid Windows distribution terms | **Include (Linux; license file present and copied into image)** |
| Gradle | gradle.org | Apache-2.0 (Gradle), docs under CC BY‑NC‑SA | **Yes** | Retain license/notice | Include |
| vcpkg | github.com/microsoft/vcpkg | MIT | **Yes** | Ports fetched by vcpkg have their own licenses (not shipped here) | Include |
| Conan | github.com/conan-io/conan | MIT | **Yes** | Conan packages have their own licenses (not shipped here) | Include |
| Codex CLI | github.com/openai/codex | Apache-2.0 | **Yes** | Retain license/notice | Include |
| Gru (gruno) | pkg.go.dev/pkt.systems/gruno | MIT | **Yes** | Retain license/notice | Include |

## Verification status
- .NET SDK: license file is present in the image and copied into `/usr/share/licenses/centaurxrunner/dotnet/` during build.

## Implementation notes
- Redistributable runner build skips Android SDK and Bun.
- Redistributable runner build verifies that the .NET SDK license and third‑party notices exist, checks that the license file contains “MIT License”, and copies both into `/usr/share/licenses/centaurxrunner/dotnet/` (build fails otherwise).

## References

Android SDK terms:
- https://developer.android.com/studio/terms

Bun licensing:
- https://bun.sh/licensing

Go license:
- https://go.dev/LICENSE

Rust licenses:
- https://www.rust-lang.org/policies/licenses

rustup license:
- https://github.com/rust-lang/rustup

Node.js license (MIT noted on official Node.js download pages):
- https://nodejs.org/download/release/v0.8.9/

.NET:
- https://dotnet.microsoft.com/en-us/dotnet  (open source / free)
- https://dotnet.microsoft.com/en-us/dotnet_library_license.htm (license terms)
- https://github.com/dotnet/core/blob/main/license-information.md (license details for binaries)

Gradle license:
- https://docs.gradle.org/current/license.html

vcpkg license:
- https://github.com/microsoft/vcpkg

Conan license:
- https://github.com/conan-io/conan

Codex license:
- https://github.com/openai/codex

Gru (gruno) license:
- https://pkg.go.dev/pkt.systems/gruno
