FROM redis:7.4-alpine@sha256:858f009f9709ce576febc734aa78b8f6d624b82571f9ddb6bda4377c833b3499
COPY --chmod=0555 docker/common/env-files.sh /usr/local/bin/layerx-env-files
COPY --chmod=0555 docker/redis/entrypoint.sh /usr/local/bin/layerx-redis
COPY docker/redis/files.tsv /etc/layerx/files.tsv
ENV LAYERX_FILES_OWNER=redis:redis
WORKDIR /data
EXPOSE 6379
ENTRYPOINT ["/usr/local/bin/layerx-env-files", "/usr/local/bin/layerx-redis"]
CMD []
