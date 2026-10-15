FROM nats:2.11.9-alpine@sha256:777787bbb4c7c4083ec2213b0b8b14151617610a3e0d85776bd995cd46599c73
COPY --chown=0:0 platform/perps-data/deploy/nats/nats.conf /etc/nats/nats.conf
RUN mkdir -p /data && chown 65532:65532 /data
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/nats-server", "--config", "/etc/nats/nats.conf"]
