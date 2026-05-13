# SPDX-License-Identifier: MIT
# syntax=docker/dockerfile:1.7

FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
COPY cmd ./cmd
COPY internal ./internal
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=linux go build \
        -trimpath \
        -ldflags="-s -w -X main.Version=$(cat /etc/issue 2>/dev/null | head -c 0)docker" \
        -o /out/pi-remote-coordinator ./cmd/pi-remote-coordinator

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/pi-remote-coordinator /usr/local/bin/pi-remote-coordinator
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/pi-remote-coordinator"]
