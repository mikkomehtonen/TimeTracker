# Multi-stage build; the app uses the pure-Go modernc.org/sqlite driver, so
# CGO stays off and the binary is fully static.
#
# Go version pinning: go.mod is the single source of truth. `make docker`
# (and therefore CI) always passes --build-arg GO_VERSION derived from go.mod,
# so that value wins; the default here only serves bare `docker build` and
# should be bumped together with go.mod.
ARG GO_VERSION=1.26.10-alpine
FROM golang:${GO_VERSION} AS build
WORKDIR /src
# Module files first so dependency downloads are cached across source changes.
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags "-s -w" \
  -o /out/timetracker ./cmd/timetracker

FROM alpine:3.24
# tzdata: `TZ` must work for the server-rendered edit-form defaults, which
# derive wall-clock values and offsets from time.Local; without tzdata the
# container silently renders UTC defaults.
RUN apk add --no-cache tzdata \
  && addgroup -S -g 10001 app && adduser -D -u 10001 -G app app \
  && mkdir -p /data && chown 10001:10001 /data
# No VOLUME declaration on purpose: with a named volume (-v) Docker seeds
# the image's /data ownership (10001:10001) on first use; without -v,
# data dies with the container instead of silently leaving anonymous volumes
# behind. Bind mounts instead inherit host directory ownership: chown the
# host dir to 10001:10001 or the app cannot open the database.
COPY --from=build /out/timetracker /usr/local/bin/timetracker
USER app
EXPOSE 8080
# HEALTHCHECK_PORT is probe-only: the server never reads it. Set it to match
# the -addr port when overriding CMD, or the probe reports a healthy server
# unhealthy. Shell-form HEALTHCHECK expands it at runtime. GET /login answers
# 200 without a session and writes nothing, so it doubles as a liveness probe.
ENV HEALTHCHECK_PORT=8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s \
  CMD wget -q --spider "http://127.0.0.1:${HEALTHCHECK_PORT}/login" || exit 1
ENTRYPOINT ["timetracker"]
CMD ["-addr", ":8080", "-data-dir", "/data"]
