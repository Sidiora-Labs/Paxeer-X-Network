# search-front: nginx in front of the serving RPC names for search.paxeer.network.
# The upstream list is not baked in; layerx-env-files decodes it from the
# SEARCH_FRONT_UPSTREAMS variable to /etc/nginx/search/upstreams.conf, real-ip
# renders the trusted proxies, and the nginx entrypoint renders the resolver of
# the container's /etc/resolv.conf (interop/deploy/search-front/railway.env.example).
FROM nginx:stable
ENV NGINX_ENTRYPOINT_LOCAL_RESOLVERS=1 \
    NGINX_ENVSUBST_OUTPUT_DIR=/etc/nginx/search/conf.d
COPY interop/deploy/search-front/nginx.conf /etc/nginx/nginx.conf
COPY docker/search-front/resolver.conf.template /etc/nginx/templates/resolver.conf.template
COPY docker/search-front/files.tsv /etc/layerx/files.tsv
COPY --chmod=0555 docker/common/env-files.sh /usr/local/bin/layerx-env-files
COPY --chmod=0555 docker/search-front/real-ip.sh /usr/local/bin/real-ip
RUN test -s /etc/ssl/certs/ca-certificates.crt && mkdir -p /etc/nginx/search/conf.d
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/layerx-env-files", "/usr/local/bin/real-ip", "/etc/nginx/search/conf.d/real-ip.conf", "/docker-entrypoint.sh"]
CMD ["nginx", "-g", "daemon off;"]
