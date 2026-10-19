ARG BUILD_IMAGE
ARG RUNTIME_IMAGE
FROM ${BUILD_IMAGE} AS build
ARG BUILD_IMAGE
ARG RUNTIME_IMAGE
ARG SERVICE
WORKDIR /src
ENV CGO_ENABLED=0 GOTOOLCHAIN=local GOWORK=off GOFLAGS=-mod=readonly SOURCE_DATE_EPOCH=0 GOCACHE=/tmp/build-cache GOMODCACHE=/tmp/mod-cache
RUN set -eu; \
    case "${BUILD_IMAGE}" in *@sha256:????????????????????????????????????????????????????????????????) ;; *) echo "BUILD_IMAGE must be pinned by digest" >&2; exit 1 ;; esac; \
    case "${RUNTIME_IMAGE}" in *@sha256:????????????????????????????????????????????????????????????????) ;; *) echo "RUNTIME_IMAGE must be pinned by digest" >&2; exit 1 ;; esac; \
    case "${SERVICE}" in perps-indexer|perps-gateway|perps-explorer|perps-portal|perps-replay|perps-loadtest) ;; *) echo "SERVICE must name a binary under services/platform/perps-data/cmd" >&2; exit 1 ;; esac
COPY go.mod go.sum ./
RUN sha256sum go.mod go.sum > /tmp/module-graph.sha256 && go mod download && go mod verify && sha256sum -c /tmp/module-graph.sha256
COPY . .
RUN go build -mod=readonly -trimpath -buildvcs=false -ldflags="-s -w -buildid=" -o /out/app "./services/platform/perps-data/cmd/${SERVICE}" && sha256sum -c /tmp/module-graph.sha256
FROM ${RUNTIME_IMAGE} AS runtime
WORKDIR /app
COPY --from=build --chown=65532:65532 /out/app /app/app
USER 65532:65532
ENTRYPOINT ["/app/app"]
