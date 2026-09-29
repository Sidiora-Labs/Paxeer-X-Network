# Builder image for every mix command run against explorer/backend.
#
# mix-in-builder.sh builds this on demand when its image reference is absent,
# and the explorer build workflow compiles the backend on the same Elixir and
# Erlang versions, so the toolchain moves in one place.
#
# Build context: the repository root
#   docker build --file docker/explorer-elixir-builder/Dockerfile \
#     --tag explorer-elixir-builder:v10.2.6 .
#
# The image carries the dependency sources and the build toolchain, not the
# compiled artefacts: mix-in-builder.sh points MIX_BUILD_PATH at a named volume
# so a rerun reuses the compilation of the environment it runs in. The umbrella
# sources are mounted over this layout at run time, which is why only the mix
# manifests are copied in.

FROM hexpm/elixir:1.19.4-erlang-27.3.4.6-alpine-3.22.2

WORKDIR /app

RUN apk --no-cache --update add \
    alpine-sdk gmp-dev automake libtool inotify-tools autoconf python3 \
    file gcompat libstdc++ curl ca-certificates git make bash postgresql-client

ENV MIX_HOME=/opt/mix

COPY explorer/backend/mix.exs explorer/backend/mix.lock ./
COPY explorer/backend/apps/block_scout_web/mix.exs ./apps/block_scout_web/
COPY explorer/backend/apps/explorer/mix.exs ./apps/explorer/
COPY explorer/backend/apps/ethereum_jsonrpc/mix.exs ./apps/ethereum_jsonrpc/
COPY explorer/backend/apps/indexer/mix.exs ./apps/indexer/
COPY explorer/backend/apps/utils/mix.exs ./apps/utils/
COPY explorer/backend/apps/nft_media_handler/mix.exs ./apps/nft_media_handler/

RUN mix local.hex --force && mix local.rebar --force
RUN mix deps.get

CMD ["/bin/sh"]
