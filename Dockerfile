# syntax=docker/dockerfile:1.7
# Monorepo build (context = repo root):
#   docker build -f platform/ai/Dockerfile -t iag-ai-platform .
# go.mod uses `replace => ../../shared/platform-go`, so the build copies the
# shared module alongside the service.

FROM golang:1.25-alpine AS build
RUN apk add --no-cache git ca-certificates
WORKDIR /src
COPY shared/platform-go ./shared/platform-go
WORKDIR /src/platform/ai
COPY platform/ai/go.mod platform/ai/go.sum ./
RUN go mod download
COPY platform/ai/ .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /ai-platform ./cmd/server

FROM alpine:3.21
RUN apk add --no-cache ca-certificates wget
WORKDIR /app
COPY --from=build /ai-platform /app/ai-platform
ENV PORT=3007
EXPOSE 3007
HEALTHCHECK --interval=15s --timeout=5s --start-period=10s --retries=5 \
  CMD wget -q -O /dev/null http://127.0.0.1:3007/health || exit 1
USER nobody
ENTRYPOINT ["/app/ai-platform"]
