#!/usr/bin/env bash
set -euo pipefail

log() { printf '[cxrunner-install] %s\n' "$*" >&2; }

install_apt_packages() {
  log "Installing base packages..."
  apt-get update
  DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
    apt-transport-https \
    bash \
    build-essential \
    ca-certificates \
    clang \
    cmake \
    chromium \
    chromium-sandbox \
    fontconfig \
    fonts-liberation \
    fonts-noto-color-emoji \
    fonts-noto-core \
    curl \
    dirmngr \
    fd-find \
    fzf \
    gdb \
    git \
    git-lfs \
    gnupg \
    gnuplot-nox \
    groovy \
    jq \
    iproute2 \
    lldb \
    lld \
    libbz2-dev \
    libcurl4-openssl-dev \
    libffi-dev \
    liblzma-dev \
    libreadline-dev \
    libsqlite3-dev \
    libssl-dev \
    libxml2-dev \
    libstdc++6 \
    libtinfo6 \
    lsof \
    maven \
    ninja-build \
    dnsutils \
    openssh-client \
    pkg-config \
    python3 \
    python3-dev \
    python3-pip \
    python3-setuptools \
    python3-venv \
    python3-wheel \
    pipx \
    r-base \
    r-base-dev \
    ripgrep \
    strace \
    tree \
    unzip \
    xz-utils \
    zip \
    zlib1g-dev
  if apt-cache show libz1 >/dev/null 2>&1; then
    log "Installing libz1 (optional)..."
    DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends libz1
  else
    log "libz1 unavailable; zlib1g is provided by zlib1g-dev."
  fi
  if DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends libncurses6; then
    log "Installed libncurses6."
  else
    log "libncurses6 unavailable; installing libncurses5."
    DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends libncurses5
  fi
  rm -rf /var/lib/apt/lists/*
  if command -v fdfind >/dev/null 2>&1 && ! command -v fd >/dev/null 2>&1; then
    ln -s /usr/bin/fdfind /usr/local/bin/fd || true
  fi
}

configure_java_home() {
  local java_home=""
  if [ -n "${JAVA_HOME:-}" ] && [ -d "${JAVA_HOME}" ]; then
    java_home="${JAVA_HOME}"
  fi
  if [ -z "$java_home" ] && command -v javac >/dev/null 2>&1; then
    local javac_path
    javac_path="$(readlink -f "$(command -v javac)")"
    java_home="$(dirname "$(dirname "$javac_path")")"
  fi
  if [ -z "$java_home" ] || [ ! -d "$java_home" ]; then
    log "Unable to determine JAVA_HOME."
    return 1
  fi
  export JAVA_HOME="$java_home"
  export PATH="${JAVA_HOME}/bin:${PATH}"
  install -d /etc/profile.d
  {
    printf 'export JAVA_HOME=%q\n' "${java_home}"
    if [ -d /usr/lib/jvm/java-21-openjdk-amd64 ]; then
      printf 'export JAVA_HOME_21=%q\n' "/usr/lib/jvm/java-21-openjdk-amd64"
    fi
    printf 'export PATH=%s\n' "${java_home}/bin:"'$PATH'
  } > /etc/profile.d/java.sh
}

install_java() {
  if command -v javac >/dev/null 2>&1; then
    log "Java already installed."
    configure_java_home
    return
  fi
  log "Installing OpenJDK..."
  apt-get update
  if DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends default-jdk-headless; then
    log "Installed default JDK."
  elif DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends openjdk-21-jdk-headless; then
    log "Installed OpenJDK 21."
  else
    log "No suitable OpenJDK packages available."
    exit 1
  fi
  rm -rf /var/lib/apt/lists/*
  configure_java_home
}

install_node() {
  if command -v node >/dev/null 2>&1; then
    log "Node already installed."
    return
  fi
  local major="${NODE_MAJOR:-20}"
  log "Installing Node.js (major ${major})..."
  curl -fsSL "https://deb.nodesource.com/setup_${major}.x" | bash -
  apt-get update
  DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends nodejs
  rm -rf /var/lib/apt/lists/*
}

install_bun() {
  if command -v bun >/dev/null 2>&1; then
    log "Bun already installed."
    return
  fi
  log "Installing Bun..."
  export BUN_INSTALL="/usr/local/bun"
  curl -fsSL https://bun.sh/install | bash
  ln -sf /usr/local/bun/bin/bun /usr/local/bin/bun
}

install_go() {
  if command -v go >/dev/null 2>&1; then
    log "Go already installed."
    return
  fi
  local version="${GO_VERSION:-latest}"
  if [ "$version" = "latest" ]; then
    version="$(curl -fsSL https://go.dev/VERSION?m=text | head -n 1 | sed 's/^go//')"
  fi
  log "Installing Go ${version}..."
  local url="https://go.dev/dl/go${version}.linux-amd64.tar.gz"
  curl -fsSL "$url" -o /tmp/go.tgz
  rm -rf /usr/local/go
  tar -C /usr/local -xzf /tmp/go.tgz
  rm -f /tmp/go.tgz
  ln -sf /usr/local/go/bin/go /usr/local/bin/go
  ln -sf /usr/local/go/bin/gofmt /usr/local/bin/gofmt
}

install_rust() {
  if command -v rustc >/dev/null 2>&1; then
    log "Rust already installed."
    return
  fi
  log "Installing Rust (rustup)..."
  export RUSTUP_HOME=/usr/local/rustup
  export CARGO_HOME=/usr/local/cargo
  curl -fsSL https://sh.rustup.rs | sh -s -- -y --profile minimal --no-modify-path
  if [ -d /usr/local/cargo/bin ]; then
    for bin in /usr/local/cargo/bin/*; do
      [ -f "$bin" ] || continue
      ln -sf "$bin" /usr/local/bin/
    done
  fi
  install -d /etc/profile.d
  {
    printf 'export RUSTUP_HOME=%q\n' "/usr/local/rustup"
    printf 'export CARGO_HOME=%q\n' "/usr/local/cargo"
  printf 'export PATH=%s\n' "/usr/local/cargo/bin:"'$PATH'
  } > /etc/profile.d/rust.sh
}

install_dotnet() {
  if command -v dotnet >/dev/null 2>&1; then
    log ".NET already installed."
    return
  fi
  local version="${DOTNET_SDK_VERSION:-8.0}"
  log "Installing .NET SDK ${version}..."
  local debian_version=""
  if [ -r /etc/os-release ]; then
    . /etc/os-release
    debian_version="${VERSION_ID:-}"
  fi
  if [ -z "$debian_version" ]; then
    log "Unable to determine Debian version for Microsoft repo."
    exit 1
  fi
  local pkg="/tmp/packages-microsoft-prod.deb"
  curl -fsSL "https://packages.microsoft.com/config/debian/${debian_version}/packages-microsoft-prod.deb" -o "$pkg"
  dpkg -i "$pkg"
  rm -f "$pkg"
  apt-get update
  DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends "dotnet-sdk-${version}"
  rm -rf /var/lib/apt/lists/*
  install -d /etc/profile.d
  {
    printf 'export DOTNET_ROOT=%q\n' "/usr/share/dotnet"
  printf 'export PATH=%s\n' "/usr/share/dotnet:"'$PATH'
  } > /etc/profile.d/dotnet.sh
}

install_gradle() {
  if command -v gradle >/dev/null 2>&1; then
    log "Gradle already installed."
    return
  fi
  log "Installing Gradle (latest)..."
  local meta="https://services.gradle.org/versions/current"
  local version
  version="$(curl -fsSL "$meta" | jq -r '.version')"
  if [ -z "$version" ] || [ "$version" = "null" ]; then
    log "Failed to resolve Gradle version."
    exit 1
  fi
  local url="https://services.gradle.org/distributions/gradle-${version}-bin.zip"
  local dest="/opt/gradle"
  local tmp="/tmp/gradle.zip"
  curl -fsSL -o "$tmp" "$url"
  rm -rf "${dest}/gradle-${version}"
  install -d "$dest"
  unzip -q "$tmp" -d "$dest"
  rm -f "$tmp"
  ln -sf "${dest}/gradle-${version}/bin/gradle" /usr/local/bin/gradle
  install -d /etc/profile.d
  {
    printf 'export GRADLE_HOME=%q\n' "${dest}/gradle-${version}"
    printf 'export PATH=%s\n' "${dest}/gradle-${version}/bin:"'$PATH'
  } > /etc/profile.d/gradle.sh
}

install_android_sdk() {
  local sdk_root="${ANDROID_SDK_ROOT:-/opt/android-sdk}"
  local sdk_home="${ANDROID_HOME:-${sdk_root}}"
  local api="${ANDROID_API:-35}"
  local build_tools="${ANDROID_BUILD_TOOLS:-35.0.0}"
  local tools_version="${ANDROID_CMDLINE_TOOLS_VERSION:-11076708}"
  local tools_zip="commandlinetools-linux-${tools_version}_latest.zip"
  local java_home="${JAVA_HOME:-}"

  if [ -x "${sdk_root}/cmdline-tools/latest/bin/sdkmanager" ]; then
    log "Android SDK already installed."
    return
  fi
  if [ -z "$java_home" ] && command -v javac >/dev/null 2>&1; then
    local javac_path
    javac_path="$(readlink -f "$(command -v javac)")"
    java_home="$(dirname "$(dirname "$javac_path")")"
  fi
  if [ -z "$java_home" ] || [ ! -d "$java_home" ]; then
    log "Java not available; skipping Android SDK install."
    return
  fi
  if command -v update-alternatives >/dev/null 2>&1; then
    update-alternatives --set java "${java_home}/bin/java" || true
    update-alternatives --set javac "${java_home}/bin/javac" || true
  fi

  log "Installing Android SDK command line tools (API ${api}, build-tools ${build_tools})..."
  install -d "${sdk_root}/cmdline-tools"
  curl -fsSL -o /tmp/cmdline-tools.zip "https://dl.google.com/android/repository/${tools_zip}"
  unzip -q /tmp/cmdline-tools.zip -d "${sdk_root}/cmdline-tools"
  rm -rf "${sdk_root}/cmdline-tools/latest"
  mv "${sdk_root}/cmdline-tools/cmdline-tools" "${sdk_root}/cmdline-tools/latest"
  rm -f /tmp/cmdline-tools.zip

  export ANDROID_SDK_ROOT="${sdk_root}"
  export ANDROID_HOME="${sdk_home}"
  export JAVA_HOME="${java_home}"
  export PATH="${JAVA_HOME}/bin:${ANDROID_SDK_ROOT}/cmdline-tools/latest/bin:${ANDROID_SDK_ROOT}/platform-tools:${ANDROID_SDK_ROOT}/emulator:${PATH}"

  log "Accepting Android SDK licenses..."
  set +o pipefail
  yes | "${ANDROID_SDK_ROOT}/cmdline-tools/latest/bin/sdkmanager" --sdk_root="${ANDROID_SDK_ROOT}" --licenses >/dev/null
  local license_rc="${PIPESTATUS[1]}"
  set -o pipefail
  if [ "$license_rc" -ne 0 ]; then
    log "sdkmanager --licenses failed (exit ${license_rc})."
    exit 1
  fi
  "${ANDROID_SDK_ROOT}/cmdline-tools/latest/bin/sdkmanager" --sdk_root="${ANDROID_SDK_ROOT}" \
    "platform-tools" \
    "platforms;android-${api}" \
    "build-tools;${build_tools}" \
    "cmdline-tools;latest" \
    "extras;google;m2repository" \
    "extras;android;m2repository"

  install -d /etc/profile.d
  {
    printf 'export ANDROID_SDK_ROOT=%q\n' "${sdk_root}"
    printf 'export ANDROID_HOME=%q\n' "${sdk_home}"
    printf 'export PATH=%s\n' "${sdk_root}/cmdline-tools/latest/bin:${sdk_root}/platform-tools:${sdk_root}/emulator:"'$PATH'
  } > /etc/profile.d/android.sh
}

install_gru() {
  if command -v gru >/dev/null 2>&1; then
    log "gru already installed."
    return
  fi
  if ! command -v go >/dev/null 2>&1; then
    log "Go not installed; skipping gru."
    return
  fi
  log "Installing gru..."
  GOBIN=/usr/local/bin go install pkt.systems/gruno/cmd/gru@latest
  ln -sf /usr/local/bin/gru /usr/local/bin/bru
}

install_vcpkg() {
  if command -v vcpkg >/dev/null 2>&1; then
    log "vcpkg already installed."
    return
  fi
  log "Installing vcpkg..."
  local dest="/opt/vcpkg"
  rm -rf "$dest"
  git clone --depth 1 https://github.com/microsoft/vcpkg "$dest"
  export VCPKG_DISABLE_METRICS=1
  (cd "$dest" && ./bootstrap-vcpkg.sh -disableMetrics)
  ln -sf "$dest/vcpkg" /usr/local/bin/vcpkg
  install -d /etc/profile.d
  printf 'export VCPKG_ROOT=%q\n' "$dest" > /etc/profile.d/vcpkg.sh
}

install_conan() {
  if command -v conan >/dev/null 2>&1; then
    log "Conan already installed."
    return
  fi
  log "Installing Conan (latest)..."
  export PIPX_HOME=/usr/local/pipx
  export PIPX_BIN_DIR=/usr/local/bin
  pipx install conan
  install -d /etc/profile.d
  printf 'export CONAN_HOME=%s\n' '$HOME/.conan2' > /etc/profile.d/conan.sh
}

verify_path_tools() {
  local ok=true
  if ! command -v ls >/dev/null 2>&1; then
    log "PATH verification failed: ls not found."
    ok=false
  fi
  if ! command -v git >/dev/null 2>&1; then
    log "PATH verification failed: git not found."
    ok=false
  fi
  if [ "$ok" != "true" ]; then
    exit 1
  fi
  log "PATH verification ok."
}

install_codex() {
  if command -v codex >/dev/null 2>&1 && [ "${FORCE_CODEX:-}" != "1" ]; then
    log "Codex already installed."
    return
  fi

  local repo="openai/codex"
  local asset="codex-x86_64-unknown-linux-musl.tar.gz"
  local prefix="${PREFIX:-/usr/bin}"

  log "Installing codex..."
  log "Repo:   ${repo}"
  log "Asset:  ${asset}"
  log "Prefix: ${prefix}"

  local tmpdir
  tmpdir="$(mktemp -d)"
  trap 'rm -rf "${tmpdir:-}"' EXIT

  local tag
  if [ -n "${TAG_OVERRIDE:-}" ]; then
    tag="${TAG_OVERRIDE}"
    log "TAG_OVERRIDE set; using ${tag}"
  else
    log "Resolving latest tag..."
    local latest="https://github.com/${repo}/releases/latest"
    local effective
    effective="$(curl -fsSL -o /dev/null -w '%{url_effective}' "$latest" || true)"
    if [[ "$effective" =~ /releases/tag/([^/?#]+) ]]; then
      tag="${BASH_REMATCH[1]}"
    else
      log "Fallback tag resolution..."
      local loc
      loc="$(
        curl -fsSI "$latest" \
          | awk 'BEGIN{IGNORECASE=1} $1=="Location:" {print $2}' \
          | tr -d '\r' \
          | tail -n 1 || true
      )"
      if [[ "$loc" =~ /releases/tag/([^/?#]+) ]]; then
        tag="${BASH_REMATCH[1]}"
      else
        log "Unable to resolve tag."
        exit 1
      fi
    fi
  fi

  local dl="https://github.com/${repo}/releases/download/${tag}/${asset}"
  local archive="${tmpdir}/${asset}"
  log "Download: ${dl}"
  curl -fL --retry 5 --retry-delay 1 -o "$archive" "$dl"

  local entry
  entry="$(tar -tzf "$archive" | head -n 1)"
  if [ -z "$entry" ]; then
    log "Archive appears empty."
    exit 1
  fi
  tar -xzf "$archive" -C "$tmpdir"
  local src="${tmpdir}/${entry}"
  if [ ! -f "$src" ]; then
    log "Expected extracted file not found: ${src}"
    exit 1
  fi
  install -m 0755 "$src" "${prefix}/codex"
  log "Codex installed."
}

install_apt_packages
install_java
install_node
install_bun
install_go
install_rust
install_dotnet
install_gradle
install_android_sdk
install_gru
install_vcpkg
install_conan
verify_path_tools
install_codex
