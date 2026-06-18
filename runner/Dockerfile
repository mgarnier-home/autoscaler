# Define base images as build arguments (allow customization in CI/CD pipelines)
ARG GO_BUILD_VERSION="docker.io/golang:bookworm"
ARG ACTIONS_IMAGE="ghcr.io/actions/actions-runner:2.335.1"

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
LABEL authors="Operis"

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
RUN printf 'Installation plugins asdf\n' \
  && sudo mkdir ${ASDF_DATA_DIR} \
  && sudo chown -R runner:runner ${ASDF_DATA_DIR} \
  && asdf plugin add 'nodejs' \
  && asdf plugin add 'deno' \
  && asdf plugin add 'java' \
  && asdf plugin add 'maven' \
  && asdf plugin add 'php' \
  && asdf plugin add 'yq' \
  && asdf plugin add 'terraform' 'https://github.com/asdf-community/asdf-hashicorp.git' \
  && printf 'Installation de yq' \
  && asdf install 'yq' '4.53.2'

COPY --chown=runner:runner ./scripts/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh
ENTRYPOINT ["/entrypoint.sh"]
