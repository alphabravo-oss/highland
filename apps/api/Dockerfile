# Multi-stage build for highland-api (static Go binary).
#
# The build stage is pinned to the *builder's* native platform ($BUILDPLATFORM)
# and Go cross-compiles for the requested $TARGETARCH. This avoids running the
# Go toolchain under QEMU emulation, so multi-arch (amd64 + arm64) builds are
# near-native speed. The final distroless stage only COPYs the binary, so it
# needs no emulation either.
FROM --platform=$BUILDPLATFORM golang:1.26.5-alpine AS build
WORKDIR /src
RUN apk add --no-cache git ca-certificates
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
ARG TARGETOS
ARG TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -trimpath -ldflags="-s -w" -o /out/highland-api ./cmd/highland-api

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /
COPY --from=build /out/highland-api /highland-api
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/highland-api"]
