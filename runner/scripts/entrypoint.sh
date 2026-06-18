#!/bin/bash
# shellcheck shell=bash

_START_DOCKER_SERVICE=${START_DOCKER_SERVICE:="false"}

_DOCKER_MIRROR_URL=${DOCKER_MIRROR_URL:-""}
_DOCKER_REGISTRY_URL=${DOCKER_REGISTRY_URL:-""}
_DOCKER_REGISTRY_USERNAME=${DOCKER_REGISTRY_USERNAME:-""}
_DOCKER_REGISTRY_PASSWORD=${DOCKER_REGISTRY_PASSWORD:-""}

# Start docker service if needed (e.g. for docker-in-docker)
# Ensure buildx, ASDF, NPM, and Maven cache directories exist with correct permissions when mounted as volumes
sudo mkdir -p \
/buildx-cache \
/home/runner/.npm \
/home/runner/.m2/repository \
/asdf/downloads
sudo chown -R runner:runner \
/buildx-cache \
/home/runner/.npm \
/home/runner/.m2/repository \
/asdf/downloads


if [[ ${_START_DOCKER_SERVICE} == "true" ]]; then
    echo "Starting docker service"
    
    # Configure dockerd startup arguments in /etc/default/docker
    if [[ -n "${_DOCKER_MIRROR_URL}" ]]; then
        echo "Configuring registry mirror in /etc/docker/daemon.json"
        
        # Remove the protocol scheme (http:// or https://) for insecure-registries
        mirror_host="${_DOCKER_MIRROR_URL#http://}"
        mirror_host="${mirror_host#https://}"
        mirror_host="${mirror_host%%/*}"
        
        # use jq to create the JSON file
        sudo mkdir -p /etc/docker
        
        jq -n --arg url "${_DOCKER_MIRROR_URL}" --arg host "${mirror_host}" '{
            "registry-mirrors": [$url],
            "insecure-registries": [$host],
            "features": {
                "containerd-snapshotter": true
            }
        }' | sudo tee /etc/docker/daemon.json > /dev/null
        
    fi
    
    sudo service docker start
    
    cat /etc/docker/daemon.json
    
    docker info
    
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
        cat > gpg_batch.cfg <<EOF
%no-protection
Key-Type: RSA
Key-Length: 4096
Subkey-Type: RSA
Subkey-Length: 4096
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
