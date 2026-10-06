# Cosmovisor Quick Start

`cosmovisor` is a small process manager for Cosmos SDK application binaries such as `paxd`. It runs the binary as a subprocess and scans its output for the upgrade message that the [`x/upgrade`](../x/upgrade/spec/README.md) module logs when an approved upgrade plan is reached:

```
UPGRADE "<name>" NEEDED at height: <height>: <info>
```

When it sees that line, `cosmovisor` stops the current binary, switches to the binary for that upgrade (downloading it first if allowed), and optionally restarts the node with the new binary.

*Note: If new versions of the application are not set up to run in-place store migrations, migrations will need to be run manually before restarting `cosmovisor` with the new binary.*

## Installation

`cosmovisor` is its own Go module (`github.com/Sidiora-Labs/Paxeer-X-Network/sdk/cosmovisor`, see [`go.mod`](go.mod)). Build it from the repository root:

```
make -C sdk cosmovisor
```

This runs `go build -mod=readonly ./cmd/cosmovisor` and leaves the `cosmovisor` binary in `sdk/cosmovisor/`. `make -C sdk/cosmovisor test` runs its tests.

## Command Line Arguments And Environment Variables

All arguments passed to `cosmovisor` are passed to the application binary (as a subprocess). `cosmovisor` writes the subprocess's stdout and stderr to its own. For this reason, `cosmovisor` does not accept any command-line arguments of its own.

`cosmovisor` reads its configuration from environment variables:

* `DAEMON_HOME` is the location where the `cosmovisor/` directory is kept that contains the genesis binary, the upgrade binaries, and any additional auxiliary files associated with each binary (e.g. `$HOME/.paxd`). It must be an absolute path.
* `DAEMON_NAME` is the name of the binary itself (e.g. `paxd`).
* `DAEMON_ALLOW_DOWNLOAD_BINARIES` (*optional*), if set to `true`, will enable auto-downloading of new binaries (for security reasons, this is intended for full nodes rather than validators). By default, `cosmovisor` will not auto-download new binaries.
* `DAEMON_RESTART_AFTER_UPGRADE` (*optional*), if set to `true`, will restart the subprocess with the same command-line arguments and flags (but with the new binary) after a successful upgrade. By default, `cosmovisor` stops running after an upgrade and requires the system administrator to manually restart it. Note that `cosmovisor` will not auto-restart the subprocess if there was an error.
* `DAEMON_LOG_BUFFER_SIZE` (*optional*) sets the size, in KiB, of the buffer used to scan each output line. Values smaller than Go's `bufio.MaxScanTokenSize` fall back to that size.

## Folder Layout

`$DAEMON_HOME/cosmovisor` is expected to belong completely to `cosmovisor` and the subprocesses that are controlled by it. The folder content is organized as follows:

```
.
├── current -> genesis or upgrades/<name>
├── genesis
│   └── bin
│       └── $DAEMON_NAME
└── upgrades
    └── <name>
        └── bin
            └── $DAEMON_NAME
```

The `cosmovisor/` directory includes a subdirectory for each version of the application (i.e. `genesis` or `upgrades/<name>`). Within each subdirectory is the application binary (i.e. `bin/$DAEMON_NAME`) and any additional auxiliary files associated with each binary. `current` is a symbolic link to the currently active directory (i.e. `genesis` or `upgrades/<name>`). The `name` variable in `upgrades/<name>` is the URI-encoded name of the upgrade as specified in the upgrade module plan.

Please note that `$DAEMON_HOME/cosmovisor` only stores the *application binaries*. The `cosmovisor` binary itself can be stored in any typical location (e.g. `/usr/local/bin`). The application will continue to store its data in the default data directory (`$HOME/.paxd` for `paxd`) or the data directory specified with the `--home` flag. `$DAEMON_HOME` is independent of the data directory and can be set to any location. If you set `$DAEMON_HOME` to the same directory as the data directory, you will end up with a configuration like the following:

```
.paxd
├── config
├── data
└── cosmovisor
```

## Usage

The system administrator is responsible for:

- installing the `cosmovisor` binary
- configuring the host's init system (e.g. `systemd`, `launchd`, etc.)
- appropriately setting the environmental variables
- manually installing the `genesis` folder
- manually installing the `upgrades/<name>` folders

`cosmovisor` will set the `current` link to point to `genesis` at first start (i.e. when no `current` link exists) and then handle switching binaries at the correct points in time, so the binaries can be placed on disk ahead of the upgrade height.

The application binary's own command-line flags and environment variables work as they do without `cosmovisor`.

## Auto-Download

Generally, `cosmovisor` requires that the system administrator place all relevant binaries on disk before the upgrade happens. However, for people who don't need such control and want an easier setup (for example a non-validating full node), there is another option.

If `DAEMON_ALLOW_DOWNLOAD_BINARIES` is set to `true`, and no local binary can be found when an upgrade is triggered, `cosmovisor` will attempt to download and install the binary itself. It does not download if `upgrades/<name>` already exists. The plan stored in the upgrade module has an info field, which is printed at the end of the upgrade message. There are two valid formats to specify a download in that field:

1. Store an os/architecture -> binary URI map in the upgrade plan info field as JSON under the `"binaries"` key. An `"any"` entry is used when there is no entry for the node's os/architecture. For example:

```json
{
  "binaries": {
    "linux/amd64": "https://<host>/paxd.zip?checksum=sha256:<sha256 hex>"
  }
}
```

2. Store a link to a file that contains all information in the above format (e.g. if you want to specify lots of binaries, changelog info, etc. without filling up the blockchain). For example:

```
https://<host>/upgrade-info.json?checksum=sha256:<sha256 hex>
```

When `cosmovisor` is triggered to download the new binary, `cosmovisor` will parse the `"binaries"` field, download the new binary with HashiCorp `go-getter`, and unpack the new binary in the `upgrades/<name>` folder so that it can be run as if it was installed manually.

Note that for this mechanism to provide strong security guarantees, all URLs should include a SHA 256/512 checksum. This ensures that no false binary is run, even if someone hacks the server or hijacks the DNS. `go-getter` will always ensure the downloaded file matches the checksum if it is provided. `go-getter` will also handle unpacking archives into directories (in this case the download link should point to a `zip` file of all data in the `bin` directory).

To create a sha256 checksum on Linux, use the `sha256sum` utility. For example, from `sdk/cosmovisor`:

```
sha256sum ./testdata/repo/zip_directory/autod.zip
```

You can also use `sha512sum` if you would prefer longer hashes. Whichever you choose, make sure to set the hash algorithm properly in the checksum argument to the URL.

## Proposing an upgrade

On the Paxeer X chain an upgrade plan is created through governance with the `software-upgrade` proposal from `x/upgrade`:

```
paxd tx gov submit-proposal software-upgrade <name> --upgrade-height <height> --upgrade-info <info> ...
```

The binary placed in `upgrades/<name>/bin` must register an upgrade handler with the same `<name>`.
