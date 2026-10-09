# syntax=docker/dockerfile:1

# ---- build stage -----------------------------------------------------------
# stdlib-only Go module: no external deps, so `go build` works offline.
FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod ./
COPY . .
ARG TARGETOS
ARG TARGETARCH
ENV CGO_ENABLED=0
RUN go build -trimpath -ldflags="-s -w" -o /out/llmctl ./cmd/llmctl

# ---- runtime stage ---------------------------------------------------------
# python3 is required by the ref-stdio backend (echo / llama-server proxy mode).
# The Go binary is static (CGO_ENABLED=0), so it runs on any base.
FROM python:3.12-slim
ENV LLMCTL_HOME=/root/.llmctl
COPY --from=build /out/llmctl /usr/local/bin/llmctl
RUN mkdir -p /root/.llmctl
# Persist installed backends, downloaded models, and state across container runs.
VOLUME ["/root/.llmctl"]
EXPOSE 8080
# One port: UI (/) + OpenAI gateway (/v1) + management API (/api/v1).
ENTRYPOINT ["llmctl", "up", "--addr", ":8080"]
