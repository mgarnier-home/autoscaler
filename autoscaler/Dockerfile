FROM docker.io/golang:1.26.4-alpine AS builder

WORKDIR /build

# Download Go module dependencies (caches them if go.mod and go.sum are not changed)
COPY go.mod go.sum ./
RUN go mod download

# Build the Go app
COPY . .
# Après le téléchargement des modules : un commit différent n'invalide pas leur cache.
ARG VERSION=dev
ARG COMMIT=none
ARG DATE=unknown
RUN go build \
    -ldflags="-X 'github.com/mgarnier-home/utils/buildinfo.Version=${VERSION}' -X 'github.com/mgarnier-home/utils/buildinfo.Commit=${COMMIT}' -X 'github.com/mgarnier-home/utils/buildinfo.Date=${DATE}'" \
    -o /build/docker-autoscaler .

FROM alpine AS runtime

WORKDIR /app

COPY --from=builder /build/docker-autoscaler /app/docker-autoscaler

EXPOSE 8080

ENTRYPOINT ["/app/docker-autoscaler"]
