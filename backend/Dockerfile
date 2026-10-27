# syntax=docker/dockerfile:1

# ---------------------------------------------------------------------------
# Build stage: compile both binaries with the module cache warm.
# ---------------------------------------------------------------------------
FROM golang:1.26.10-alpine AS build

WORKDIR /src

# Dependencies first so that source edits do not invalidate the module layer.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

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
# is never served over HTTP.
RUN mkdir -p /var/lib/forgecrm/imports /var/lib/forgecrm/tmp \
    && chown -R forgecrm:forgecrm /var/lib/forgecrm

COPY --from=build /out/forgecrm-api /usr/local/bin/forgecrm-api
COPY --from=build /out/forgecrm-worker /usr/local/bin/forgecrm-worker

USER 10001:10001

EXPOSE 8080

# The API image is the default; the worker overrides the entrypoint in compose.
ENTRYPOINT ["/usr/local/bin/forgecrm-api"]
