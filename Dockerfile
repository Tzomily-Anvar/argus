# Two build stages, one tiny result.
#
# The frontend and the Go binary are built here, so nobody cloning this
# needs Node or a Go toolchain installed - only Docker. The final image
# carries a single static binary on distroless/static: no shell, no
# package manager, no interpreter, nothing to patch. It runs as a
# non-root user and starts instantly.

# ---- 1. build the dashboard -------------------------------------------
FROM node:22-alpine AS web
WORKDIR /web

# Copy the manifests first so `npm ci` is cached until dependencies
# actually change, not on every source edit.
COPY web/package.json web/package-lock.json* ./
RUN npm ci --no-audit --no-fund

COPY web/ ./
RUN npm run build

# ---- 2. build the binary ----------------------------------------------
# Must be at least the `go` directive in go.mod, which a dependency can
# raise: adding goose moved it to 1.26 and this line did not follow, so
# the image stopped building while every other check still passed. CI
# builds the image for exactly that reason.
FROM golang:1.26-alpine AS build
WORKDIR /src

# Both files: go mod download verifies every module against go.sum, so
# copying only go.mod fails the moment the project has a dependency.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# The built dashboard has to be in place before `go build`, because it is
# embedded into the binary by web/embed.go.
COPY --from=web /web/dist ./web/dist

# CGO_ENABLED=0 produces a fully static binary, which is what allows the
# scratch-like final stage. -s -w strips debug info; this is a dashboard,
# not something you attach a debugger to in production.
ENV CGO_ENABLED=0 GOOS=linux
RUN go build -trimpath -ldflags="-s -w" -o /argus ./cmd/argus

# ---- 3. ship ----------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /argus /argus

# Not a secret and not org-specific: just the port the server listens on.
ENV ARGUS_PORT=18474
EXPOSE 18474

USER nonroot:nonroot

# Exec form, because distroless has no shell to interpret the string
# form - and no curl either, which is why the binary probes itself.
# Declared here as well as in compose so `docker run` gets it too.
HEALTHCHECK --interval=60s --timeout=5s --retries=3 --start-period=10s \
  CMD ["/argus", "-healthcheck"]

ENTRYPOINT ["/argus"]
