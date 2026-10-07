# hpx-registry — tiny stdlib-only Go service behind nginx. Artifacts and registry data live on the volume
# at /srv/hpx (not baked), so re-publishing is just a file copy.
FROM golang:1.22-alpine AS build
ARG SOURCE_REVISION=unknown
WORKDIR /src
COPY hpx/registry/go.mod ./
COPY hpx/registry/main.go ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.sourceRevision=${SOURCE_REVISION}" -o /hpx-registry .

# The landing page fonts, checksum-bound by hpx/hosting/fonts.sha256 as
# hpx/hosting/deploy.sh fetches them.
FROM alpine:3.20 AS fonts
WORKDIR /fonts
COPY hpx/hosting/fonts.sha256 ./
RUN apk add --no-cache curl \
    && for font in $(awk '{print $2}' fonts.sha256); do \
        curl -fsSL --retry 5 --max-time 60 -o "$font" "https://cdn.usercontent.paxeercode.com/fonts/$font"; \
    done \
    && sha256sum -c fonts.sha256 \
    && rm fonts.sha256

FROM alpine:3.20
ARG SOURCE_REVISION=unknown
LABEL org.opencontainers.image.source="https://github.com/Sidiora-Labs/Paxeer-X-Network" \
      org.opencontainers.image.revision="${SOURCE_REVISION}" \
      org.opencontainers.image.title="HyperPax HPX Registry"
RUN apk add --no-cache nginx su-exec \
    && adduser -D -u 10001 hpx \
    && rm -f /etc/nginx/http.d/default.conf
COPY --from=build /hpx-registry /usr/local/bin/hpx-registry
COPY --from=fonts /fonts/ /var/www/hpx/fonts/
COPY hpx/hosting/index.html /var/www/hpx/index.html
COPY assets/PaxeerLogo.png /var/www/hpx/paxeer-logo.png
COPY hpx/hosting/nginx-registry.conf.template /etc/nginx/hpx-registry.conf.template
COPY --chmod=0555 docker/search-front/real-ip.sh /usr/local/bin/real-ip
COPY docker/hpx-registry/entrypoint.sh /usr/local/bin/hpx-entrypoint
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/real-ip", "/etc/nginx/hpx-real-ip.conf", "/usr/local/bin/hpx-entrypoint"]
