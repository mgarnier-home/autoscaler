FROM docker.io/golang:1.26.4-alpine AS builder

WORKDIR /build

# Download Go module dependencies (caches them if go.mod and go.sum are not changed)
COPY go.mod go.sum ./
RUN go mod download

# Build the Go app
COPY . .
RUN go build -o /build/docker-autoscaler .

FROM alpine AS runtime

WORKDIR /app

COPY --from=builder /build/docker-autoscaler /app/docker-autoscaler

ENTRYPOINT ["/app/docker-autoscaler"]
