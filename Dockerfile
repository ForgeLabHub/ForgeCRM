# ---------------------------------------------------------------------------
# Root build file for platforms that build from the repository root
# (blitz.cloud, and any host that expects a top-level Dockerfile).
#
# It produces one image with three things: the API binary, the worker binary and
# the built single-page web interface. The API serves the interface from
# /app/public (see FORGECRM_STATIC_DIR), so a single container answers both the
# API and the UI. backend/Dockerfile still builds the backend-only image used by
# docker-compose.yml; this file adds the frontend stage on top.
# ---------------------------------------------------------------------------

# ---------------------------------------------------------------------------
# Stage 1: build the web interface.
# ---------------------------------------------------------------------------
FROM node:22-alpine AS frontend

WORKDIR /web

# Dependencies first so a source edit does not reinstall them.
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci

COPY frontend/ ./
RUN npm run build

# ---------------------------------------------------------------------------
# Stage 2: resolve the build revision from the checkout.
#
# The revision is read from the git checkout, not from an environment variable,
# because a variable is maintained by hand and goes stale: a platform that builds
# the repository root (blitz.cloud) passes no docker build arguments, so the image
# used to fall back to a manually set FORGECRM_COMMIT that still named an older
# commit than the one actually deployed. The checkout is the single source of
# truth for "what is being built"; the environment remains a fallback for local
# development, where no checkout is available inside the image.
#
# The context is copied into a throwaway stage so this step cannot fail a build:
# `COPY . /meta` always matches, and a context without .git (a source tarball
# rather than a clone) yields the "unknown" marker instead of a broken image.
# The runtime image never sees this stage.
# ---------------------------------------------------------------------------
FROM alpine:3.20 AS buildmeta

RUN apk add --no-cache git

WORKDIR /meta
COPY . /meta
RUN set -eu; \
    if [ -d /meta/.git ]; then \
        git config --global --add safe.directory /meta; \
        git -C /meta rev-parse HEAD > /revision; \
    else \
        printf 'unknown' > /revision; \
    fi

# ---------------------------------------------------------------------------
# Stage 3: compile both Go binaries.
# ---------------------------------------------------------------------------
FROM golang:1.26.10-alpine AS build

WORKDIR /src

COPY backend/go.mod backend/go.sum ./
RUN go mod download

COPY backend/ ./

# The revision resolved from the checkout. A build argument still wins, so a
# release pipeline can pin it; only when neither is present does main.go fall
# back to the environment (local development).
COPY --from=buildmeta /revision /revision

ARG VERSION=dev
ARG COMMIT=

RUN set -eu; \
    revision="${COMMIT:-$(cat /revision)}"; \
    CGO_ENABLED=0 GOOS=linux go build \
        -trimpath \
        -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${revision}" \
        -o /out/forgecrm-api ./cmd/forgecrm-api \
    && CGO_ENABLED=0 GOOS=linux go build \
        -trimpath \
        -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${revision}" \
        -o /out/forgecrm-worker ./cmd/forgecrm-worker

# ---------------------------------------------------------------------------
# Stage 4: minimal runtime image, non-root, no shell.
# ---------------------------------------------------------------------------
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -g 10001 -S forgecrm \
    && adduser -u 10001 -S forgecrm -G forgecrm

# Uploaded files and extracted archives live under /var/lib/forgecrm; it is a
# volume in compose and is never served over HTTP. The web interface is static
# and is served from /app/public.
RUN mkdir -p /var/lib/forgecrm/imports /var/lib/forgecrm/tmp /app/public \
    && chown -R forgecrm:forgecrm /var/lib/forgecrm /app

COPY --from=build /out/forgecrm-api /usr/local/bin/forgecrm-api
COPY --from=build /out/forgecrm-worker /usr/local/bin/forgecrm-worker
COPY --from=frontend /web/dist /app/public

# The entrypoint chooses the binary. One image holds both, and a platform that
# sets its "docker command" to the worker path appends it to the entrypoint
# rather than replacing it, so the dispatcher turns that into the worker instead
# of a second API process. See deploy/entrypoint.sh.
COPY deploy/entrypoint.sh /usr/local/bin/forgecrm-entrypoint
RUN chmod 0755 /usr/local/bin/forgecrm-entrypoint

# /app is the working directory so the interface's default location ("public")
# resolves to /app/public without requiring FORGECRM_STATIC_DIR to be set.
WORKDIR /app

USER 10001:10001

EXPOSE 8080

# The API is the default binary; the dispatcher selects the worker when asked.
ENTRYPOINT ["/usr/local/bin/forgecrm-entrypoint"]
