FROM docker.io/golang:1.26.4-alpine AS builder

WORKDIR /build

COPY . .

RUN go build -o /build/docker-autoscaler .

FROM alpine:3.18

WORKDIR /app

COPY --from=builder /build/docker-autoscaler /app/docker-autoscaler

ENTRYPOINT ["/app/docker-autoscaler"]
