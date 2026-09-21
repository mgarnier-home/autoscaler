#!/bin/bash
# shellcheck shell=bash

_START_DOCKER_SERVICE=${START_DOCKER_SERVICE:="false"}

_DOCKER_REGISTRY_URL=${DOCKER_REGISTRY_URL:-""}
_DOCKER_REGISTRY_USERNAME=${DOCKER_REGISTRY_USERNAME:-""}
_DOCKER_REGISTRY_PASSWORD=${DOCKER_REGISTRY_PASSWORD:-""}

# URL of the buildkit daemon shared between runners (e.g. tcp://buildkit:1234).
# Left empty, the runner keeps its local buildx builder.
_BUILDKIT_HOST_URL=${BUILDKIT_HOST_URL:-""}
_BUILDKIT_BUILDER_NAME="remote-buildkit"

# Configure buildx to use the shared buildkit daemon, so that the build cache is
# kept between runners. Falls back to the local builder if buildkit is unreachable:
# a shared cache is an optimisation, not a single point of failure for every runner.
configure_remote_builder() {
    if [[ -z "${_BUILDKIT_HOST_URL}" ]]; then
        echo "BUILDKIT_HOST_URL is not set. Keeping the local buildx builder."
        return 0
    fi
    
    echo "Creating remote buildx builder on ${_BUILDKIT_HOST_URL}"
    
    # A builder of the same name may linger in /home/runner/.docker/buildx
    docker buildx rm "${_BUILDKIT_BUILDER_NAME}" >/dev/null 2>&1 || true
    
    # --bootstrap queries the remote daemon and fails if it is unreachable.
    # Note that a failed bootstrap still leaves the broken builder selected,
    # hence the explicit cleanup below.
    if docker buildx create \
    --name "${_BUILDKIT_BUILDER_NAME}" \
    --driver remote \
    --use \
    --bootstrap \
    "${_BUILDKIT_HOST_URL}"; then
        docker buildx inspect
    else
        echo "WARN: buildkit unreachable at ${_BUILDKIT_HOST_URL}. Falling back to the local builder."
        docker buildx rm "${_BUILDKIT_BUILDER_NAME}" >/dev/null 2>&1 || true
        docker buildx use default || true
    fi
}

# Start docker service if needed (e.g. for docker-in-docker)
# Ensure buildx, ASDF, NPM, and Maven cache directories exist with correct permissions when mounted as volumes
sudo mkdir -p \
/home/runner/.npm \
/asdf/downloads
sudo chown -R runner:runner \
/home/runner/.npm \
/asdf/downloads


if [[ ${_START_DOCKER_SERVICE} == "true" ]]; then
    echo "Starting docker service"
    
    sudo test -f /etc/docker/daemon.json || echo '{}' | sudo tee /etc/docker/daemon.json >/dev/null
    
    tmpfile=$(mktemp)
    
    # Enable the containerd snapshotter feature
    jq \
    '.features["containerd-snapshotter"] = true' \
    /etc/docker/daemon.json > "${tmpfile}"
    
    sudo mv "${tmpfile}" /etc/docker/daemon.json
    
    sudo service docker start
    
    cat /etc/docker/daemon.json
    
    docker info
    
    if [[ -f /opt/buildkit-image.tar ]]; then
        echo "Preloading buildx builder image from /opt/buildkit-image.tar"
        docker load -i /opt/buildkit-image.tar
    fi
    
    if [[ -z "${_DOCKER_REGISTRY_URL}" ]] || [[ -z "${_DOCKER_REGISTRY_USERNAME}" ]] || [[ -z "${_DOCKER_REGISTRY_PASSWORD}" ]]; then
        echo "DOCKER_REGISTRY_URL, DOCKER_REGISTRY_USERNAME or DOCKER_REGISTRY_PASSWORD is not set. Skipping docker login."
    else
        echo "Configuring docker credential helper for pass"
        
        # Set up docker credential helper for pass
        mkdir -p /home/runner/.docker
        echo '{"credsStore":"pass"}' > /home/runner/.docker/config.json
        chown -R runner:runner /home/runner/.docker
        chmod 700 /home/runner/.docker
        chmod 600 /home/runner/.docker/config.json
        
        
        # Generate GPG key batch file
        # Ed25519 keys generate near-instantly, unlike RSA 4096 which is slow on every startup
        cat > gpg_batch.cfg <<EOF
%no-protection
Key-Type: EDDSA
Key-Curve: ed25519
Subkey-Type: EDDSA
Subkey-Curve: ed25519
Name-Real: Docker Credential Pass Key
Name-Email: docker-pass@example.com
Expire-Date: 0
%commit
EOF
        
        gpg2 --batch --gen-key gpg_batch.cfg
        KEY_ID=$(gpg2 --list-secret-keys --with-colons | awk -F: '/^sec/{print $5; exit}')
        
        # Initialize pass with the generated GPG key
        pass init "${KEY_ID}"
        echo "Logging into docker registry ${_DOCKER_REGISTRY_URL}"
        
        echo "${_DOCKER_REGISTRY_PASSWORD}" | docker login "${_DOCKER_REGISTRY_URL}" -u "${_DOCKER_REGISTRY_USERNAME}" --password-stdin
    fi
    
    configure_remote_builder
fi



unset_config_vars() {
    echo "Unsetting configuration environment variables"
    unset START_DOCKER_SERVICE
    unset DOCKER_REGISTRY_URL
    unset DOCKER_REGISTRY_USERNAME
    unset DOCKER_REGISTRY_PASSWORD
}

unset_config_vars

"$@"

echo "Runner script completed successfully."

