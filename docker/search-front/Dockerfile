# search-front: nginx in front of the serving RPC names for search.paxeer.network.
# The upstream list is not baked in; Fly mounts it from a secret at
# /etc/nginx/search/upstreams.conf (interop/deploy/search-front/fly.toml).
FROM nginx:stable
COPY interop/deploy/search-front/nginx.conf /etc/nginx/nginx.conf
RUN test -s /etc/ssl/certs/ca-certificates.crt && mkdir -p /etc/nginx/search
EXPOSE 8080
