# Multi-stage build for highland-api (static Go binary)
FROM golang:1.22-alpine AS build
WORKDIR /src
RUN apk add --no-cache git ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/highland-api ./cmd/highland-api

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /
COPY --from=build /out/highland-api /highland-api
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/highland-api"]
