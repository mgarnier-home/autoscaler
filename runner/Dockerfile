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

RUN printf 'Installation plugins et outils via asdf\n' \
  && sudo mkdir ${ASDF_DATA_DIR} \
  && sudo chown -R runner:runner ${ASDF_DATA_DIR} \
  && asdf plugin add 'nodejs' \
  && asdf plugin add 'deno' \
  && asdf plugin add 'golang' \
  && asdf plugin add 'terraform' 'https://github.com/asdf-community/asdf-hashicorp.git' \
  && asdf install 'golang' '1.26.4' \
  && asdf install 'terraform' '1.16.3' \
  && asdf install 'nodejs' '24.21.0' \
  && asdf install 'golang' '1.26.4'


COPY --chown=runner:runner ./scripts/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh
ENTRYPOINT ["/entrypoint.sh"]
