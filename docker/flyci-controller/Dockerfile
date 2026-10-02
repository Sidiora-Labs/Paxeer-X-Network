FROM docker.io/golang:1.25.6-bookworm@sha256:2f768d462dbffbb0f0b3a5171009f162945b086f326e0b2a8fd5d29c3219ff14 AS build
WORKDIR /src
COPY go.mod go.sum ./
COPY tools/flyci/controller ./tools/flyci/controller
ARG SOURCE_REVISION
RUN test "${#SOURCE_REVISION}" -eq 40 && case "$SOURCE_REVISION" in *[!0-9a-f]*) exit 1;; esac \
    && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w -X main.ciSourceRevision=$SOURCE_REVISION" -o /out/flyci-controller ./tools/flyci/controller

FROM debian:bookworm-slim@sha256:88200866dfff7ea7f5cbcb6ec7c8a701889efe6fe859fe64d6990e4b07ea4171
ARG SOURCE_REVISION
LABEL org.opencontainers.image.revision=$SOURCE_REVISION
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/flyci-controller /usr/local/bin/flyci-controller
USER 65532:65532
CMD ["/usr/local/bin/flyci-controller"]
