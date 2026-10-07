FROM golang:1.25 AS build
WORKDIR /src
COPY . .
WORKDIR /src/human/wallet/attestor
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/attestor ./cmd/attestor

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd -g 65532 nonroot \
    && useradd -u 65532 -g 65532 -M -d /nonexistent -s /usr/sbin/nologin nonroot \
    && mkdir -p /run/secrets /data /etc/attestor \
    && chown 65532:65532 /run/secrets /data \
    && chmod 0700 /run/secrets
COPY --chmod=0555 docker/common/env-files.sh /usr/local/bin/layerx-env-files
COPY docker/wallet-attestor/files.tsv /etc/layerx/files.tsv
COPY human/wallet/deploy/attestor-policy.json /etc/attestor/policy.json
COPY human/wallet/deploy/attestor-kernel-policy.json /etc/attestor/kernel-policy.json
COPY --from=build /out/attestor /usr/local/bin/attestor
USER nonroot:nonroot
EXPOSE 8443 9443
VOLUME ["/data"]
ENTRYPOINT ["/usr/local/bin/layerx-env-files", "/usr/local/bin/attestor"]
