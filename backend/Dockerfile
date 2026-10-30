# syntax=docker/dockerfile:1

# ---------------------------------------------------------------------------
# Frontend build stage: build React web interface.
# ---------------------------------------------------------------------------
FROM node:22-alpine AS frontend-build

WORKDIR /src/frontend
COPY frontend/package*.json ./
RUN npm ci
COPY frontend/ .
RUN npm run build

# ---------------------------------------------------------------------------
# Go build stage: compile both binaries with the module cache warm.
# ---------------------------------------------------------------------------
FROM golang:1.26.10-alpine AS build

WORKDIR /src

# Dependencies first so that source edits do not invalidate the module layer.
COPY backend/go.mod backend/go.sum ./
RUN go mod download

COPY backend/ .

ARG VERSION=dev
ARG COMMIT=unknown

RUN CGO_ENABLED=0 GOOS=linux go build \
        -trimpath \
        -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
        -o /out/forgecrm-api ./cmd/forgecrm-api \
    && CGO_ENABLED=0 GOOS=linux go build \
        -trimpath \
        -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
        -o /out/forgecrm-worker ./cmd/forgecrm-worker

# ---------------------------------------------------------------------------
# Runtime stage: distroless-style minimal image, non-root, no shell.
# ---------------------------------------------------------------------------
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -g 10001 -S forgecrm \
    && adduser -u 10001 -S forgecrm -G forgecrm

# Uploaded files and extracted archives live here. It is a volume in compose and
# is never served over HTTP. Static SPA files live in /app/public.
RUN mkdir -p /var/lib/forgecrm/imports /var/lib/forgecrm/tmp /app/public \
    && chown -R forgecrm:forgecrm /var/lib/forgecrm /app

COPY --from=build /out/forgecrm-api /usr/local/bin/forgecrm-api
COPY --from=build /out/forgecrm-worker /usr/local/bin/forgecrm-worker
COPY --from=frontend-build /src/frontend/dist /app/public

ENV FORGECRM_STATIC_DIR=/app/public

USER 10001:10001

EXPOSE 8080

# The API image is the default; the worker overrides the entrypoint in compose.
ENTRYPOINT ["/usr/local/bin/forgecrm-api"]
