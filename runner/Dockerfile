# Define base images as build arguments (allow customization in CI/CD pipelines)
ARG GO_BUILD_VERSION="docker.io/golang:bookworm"
ARG ACTIONS_IMAGE="ghcr.io/actions/actions-runner:2.337.0"

# ---- Build Stage ----
FROM ${GO_BUILD_VERSION} AS builder

# Set working directory inside the container
WORKDIR /go

# Define ASDF version (must be set when building a derived image)
ARG ASDF_VERSION="v0.18.0"

# Build ASDF Go binary (ASDF is a Go-based CLI tool)
RUN go install "github.com/asdf-vm/asdf/cmd/asdf@${ASDF_VERSION}"

# ---- Final Stage (Minimal Runtime Image) ----
FROM ${ACTIONS_IMAGE}
LABEL authors="mgarnier11"

# Install packages, docker, gh cli and az cli
USER root
COPY ./scripts/install.sh /install.sh
RUN /bin/bash /install.sh && rm /install.sh
USER runner

# Set ASDF-related environment variables (ensures proper path resolution)
ENV ASDF_DATA_DIR="/asdf"
ENV PATH="${PATH}:/asdf/shims:/asdf/bin"

# Copy the ASDF binary from the builder stage
COPY --from=builder /go/bin/asdf /usr/local/bin/asdf

# Install asdf plugins
COPY ./scripts/asdf-plugins.sh /usr/local/bin/asdf-plugins.sh
RUN sudo chmod +x /usr/local/bin/asdf-plugins.sh \
  && printf 'Installation plugins asdf\n' \
  && sudo mkdir ${ASDF_DATA_DIR} \
  && sudo chown -R runner:runner ${ASDF_DATA_DIR} \
  && /usr/local/bin/asdf-plugins.sh

# Pré-installation des versions standard. Elles finissent dans un layer docker,
# donc partagées par tous les runners de l'hôte, en lecture seule et sans
# concurrence possible : c'est le cache d'outils du runner.
#
# `asdf install` sans argument lit le .tool-versions du répertoire courant,
# installe chaque outil et génère les shims correspondants. Le fichier est
# ensuite supprimé pour ne pas devenir la version par défaut des jobs.
COPY --chown=runner:runner ./tool-versions /tmp/asdf-preinstall/.tool-versions
# Le `cd /` avant le `rm` n'est pas cosmétique : supprimer le répertoire courant
# fait échouer tout binaire lancé ensuite sur `getwd: no such file or directory`.
RUN cd /tmp/asdf-preinstall \
  && printf 'Pre-installation des versions standard\n' \
  && asdf install \
  && cd / \
  && rm -rf /tmp/asdf-preinstall \
  && asdf list


COPY --chown=runner:runner ./scripts/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh
ENTRYPOINT ["/entrypoint.sh"]
