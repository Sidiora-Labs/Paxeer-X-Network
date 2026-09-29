FROM ubuntu:24.04@sha256:008173c23f95b170204355c12626cb5a965d779a7e1283b09e9cffbb1bf33ca3

ARG RUNNER_VERSION=2.337.0
ARG RUNNER_SHA256=70920811a4f8ad4328818682bca5c6469c1c942fab52448868071d0063816613

ENV DEBIAN_FRONTEND=noninteractive \
    LANG=en_US.UTF-8 \
    LC_ALL=en_US.UTF-8 \
    RUNNER_TOOL_CACHE=/opt/hostedtoolcache \
    AGENT_TOOLSDIRECTORY=/opt/hostedtoolcache \
    RUNNER_IDLE_TIMEOUT=900 \
    PATH=/home/runner/.cargo/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        authbind \
        bc \
        build-essential \
        ca-certificates \
        clang \
        cmake \
        curl \
        dbus \
        dbus-x11 \
        file \
        git \
        git-lfs \
        gnome-keyring \
        gnupg \
        jq \
        libbz2-dev \
        libclang-dev \
        libgflags-dev \
        libjemalloc-dev \
        liblz4-dev \
        libnss3-tools \
        libsnappy-dev \
        libsqlite3-dev \
        libssl-dev \
        liburing-dev \
        libzstd-dev \
        lld \
        llvm \
        locales \
        ninja-build \
        openssl \
        pkg-config \
        protobuf-compiler \
        python3 \
        python3-pip \
        python3-pytest \
        python3-venv \
        python3-yaml \
        redis-server \
        ripgrep \
        rsync \
        socat \
        sudo \
        tzdata \
        unzip \
        xz-utils \
        zip \
        zlib1g-dev \
        zstd \
    && locale-gen en_US.UTF-8 \
    && rm -rf /var/lib/apt/lists/*

RUN userdel --remove ubuntu \
    && useradd --create-home --uid 1001 --shell /bin/bash runner \
    && printf 'runner ALL=(ALL) NOPASSWD:ALL\nDefaults:runner !requiretty\n' > /etc/sudoers.d/runner \
    && chmod 0440 /etc/sudoers.d/runner \
    && visudo --check --file=/etc/sudoers.d/runner \
    && install -d -o runner -g runner -m 0755 /opt/hostedtoolcache /home/runner/actions-runner

WORKDIR /home/runner/actions-runner

RUN curl --fail --silent --show-error --location --output /tmp/actions-runner.tar.gz \
        "https://github.com/actions/runner/releases/download/v${RUNNER_VERSION}/actions-runner-linux-x64-${RUNNER_VERSION}.tar.gz" \
    && printf '%s  %s\n' "$RUNNER_SHA256" /tmp/actions-runner.tar.gz | sha256sum --check --strict \
    && tar -xzf /tmp/actions-runner.tar.gz -C /home/runner/actions-runner \
    && rm -f /tmp/actions-runner.tar.gz \
    && apt-get update \
    && ./bin/installdependencies.sh \
    && rm -rf /var/lib/apt/lists/* \
    && chown -R runner:runner /home/runner/actions-runner

RUN curl --fail --silent --show-error --location --output /tmp/rustup-init \
        https://static.rust-lang.org/rustup/dist/x86_64-unknown-linux-gnu/rustup-init \
    && curl --fail --silent --show-error --location --output /tmp/rustup-init.sha256 \
        https://static.rust-lang.org/rustup/dist/x86_64-unknown-linux-gnu/rustup-init.sha256 \
    && printf '%s  %s\n' "$(cut -d ' ' -f 1 /tmp/rustup-init.sha256)" /tmp/rustup-init | sha256sum --check --strict \
    && chmod 0755 /tmp/rustup-init \
    && su --login runner -c '/tmp/rustup-init -y --no-modify-path --profile minimal --default-toolchain stable --target wasm32-unknown-unknown' \
    && rm -f /tmp/rustup-init /tmp/rustup-init.sha256

COPY --chmod=0755 tools/flyci/runner/entrypoint.sh /usr/local/bin/entrypoint.sh

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
