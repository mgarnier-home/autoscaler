#!/usr/bin/env bash

set -euxo pipefail

# Detect architecture and download the correct binary
ARCH=$(dpkg --print-architecture)
case "$ARCH" in
    amd64)
        DOCKER_ARCH="amd64"
    ;;
    arm64)
        DOCKER_ARCH="arm64"
    ;;
    *)
        echo "Unsupported architecture: $ARCH"
        exit 1
    ;;
esac

main() (
    install_prerequisites
    
    configure_gh_cli
    configure_docker
    
    apt-get update
    update-ca-certificates
    
    install_az_cli
    install_gh_cli
    install_docker
    
    setup_sudoers
    create_user_runner
    
    configure_docker_credential_helpers
    chown -R runner:runner /home/runner
)

configure_docker() {
    # shellcheck source=/dev/null
    source /etc/os-release
    
    mkdir -p /etc/apt/keyrings
    curl -fsSL "https://download.docker.com/linux/$ID/gpg" | gpg --dearmor -o /etc/apt/keyrings/docker.gpg
    
    local version
    version=$(echo "$VERSION_CODENAME" | sed 's/trixie\|n\/a/bookworm/g')
    echo "deb [arch=${DOCKER_ARCH} signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/$ID ${version} stable" \
    | tee /etc/apt/sources.list.d/docker.list > /dev/null
}

configure_gh_cli() {
    (type -p wget >/dev/null || (apt update && apt install wget -y)) \
    && mkdir -p -m 755 /etc/apt/keyrings \
    && out=$(mktemp) && wget -nv -O "$out" https://cli.github.com/packages/githubcli-archive-keyring.gpg \
    && cat "$out" | tee /etc/apt/keyrings/githubcli-archive-keyring.gpg > /dev/null \
    && chmod go+r /etc/apt/keyrings/githubcli-archive-keyring.gpg \
    && mkdir -p -m 755 /etc/apt/sources.list.d \
    && echo "deb [arch=${DOCKER_ARCH} signed-by=/etc/apt/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" | tee /etc/apt/sources.list.d/github-cli.list > /dev/null
}

install_prerequisites() {
    apt-get update
    
    DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    gnupg \
    tar \
    unzip \
    zip \
    apt-transport-https \
    sudo \
    dirmngr \
    locales \
    gosu \
    git \
    gpg-agent \
    dumb-init \
    libc-bin \
    pass \
    zip \
    openssh-client \
    jq \
    gnupg2
}

install_docker() {
    # https://github.com/nestybox/sysbox/issues/1011
    # On fixe la version de docker à 28 tant que l'issue ci dessus n'est pas résolue
    local DOCKER_ENGINE_VERSION CONTAINERD_VERSION DOCKER_BUILDX_VERSION DOCKER_COMPOSE_PLUGIN_VERSION
    DOCKER_ENGINE_VERSION="5:28.5.2-1~ubuntu.24.04~noble"
    CONTAINERD_VERSION="1.7.29-1~ubuntu.24.04~noble"
    DOCKER_BUILDX_VERSION="0.34.1-1~ubuntu.24.04~noble"
    DOCKER_COMPOSE_PLUGIN_VERSION="2.40.3-1~ubuntu.24.04~noble"
    
    apt-get install -y \
    --allow-downgrades \
    --no-install-recommends \
    --allow-unauthenticated \
    docker-ce="${DOCKER_ENGINE_VERSION}" \
    docker-ce-cli="${DOCKER_ENGINE_VERSION}" \
    containerd.io="${CONTAINERD_VERSION}" \
    docker-buildx-plugin="${DOCKER_BUILDX_VERSION}" \
    docker-compose-plugin="${DOCKER_COMPOSE_PLUGIN_VERSION}"
    
    # runc >= 1.2.0 has strict procfs validation (openat2 RESOLVE_NO_XDEV) that
    # is incompatible with sysbox's /proc/sys FUSE virtualisation, causing:
    #   "unsafe procfs detected … invalid cross-device link"
    # Even pinning containerd.io 1.7.x may ship runc 1.2.x in newer patches,
    # so we explicitly replace the runc binary with a known 1.1.x release.
    # Replace runc with 1.1.x to ensure sysbox compatibility
    RUNC_VERSION="1.1.15"
    echo "Replacing runc with v${RUNC_VERSION} for sysbox compatibility"
    RUNC_PATH=$(which runc)
    wget -O "${RUNC_PATH}" "https://github.com/opencontainers/runc/releases/download/v${RUNC_VERSION}/runc.${DOCKER_ARCH}"
    chmod +x "${RUNC_PATH}"
    
    # Verify
    INSTALLED_RUNC=$(runc --version | head -1)
    echo "Installed: ${INSTALLED_RUNC}"
    
    echo -e '#!/bin/sh\ndocker compose --compatibility "$@"' > /usr/local/bin/docker-compose
    chmod +x /usr/local/bin/docker-compose
    
    sed -i 's/ulimit -Hn/# ulimit -Hn/g' /etc/init.d/docker
}

configure_docker_credential_helpers() {
    wget -O docker-credential-pass "https://github.com/docker/docker-credential-helpers/releases/download/v0.9.4/docker-credential-pass-v0.9.4.linux-${DOCKER_ARCH}"
    chmod +x docker-credential-pass
    mv docker-credential-pass /usr/local/bin/
}

install_gh_cli() {
    apt install -y gh
}

install_az_cli() {
    script_path=/tmp/az_install.sh
    curl \
    --fail \
    --silent \
    --show-error \
    --location \
    'https://azurecliprod.blob.core.windows.net/$root/deb_install.sh' \
    --output "${script_path}"
    bash "${script_path}" -y
    rm "${script_path}"
}

setup_sudoers() {
    sed -e 's/Defaults.*env_reset/Defaults env_keep = "HTTP_PROXY HTTPS_PROXY NO_PROXY FTP_PROXY http_proxy https_proxy no_proxy ftp_proxy"/' -i /etc/sudoers
    echo '%sudo ALL=(ALL) NOPASSWD: ALL' >> /etc/sudoers
}

create_user_runner() {
    # groupadd -g "121" runner
    # useradd -mr -d /home/runner -u "1001" -g "121" runner
    usermod -aG sudo runner
    usermod -aG docker runner
}

main "$@"
