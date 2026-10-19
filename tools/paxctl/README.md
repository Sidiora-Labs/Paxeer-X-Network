# :control_knobs: `seictl`

> Sei node operators' best friend

A command-line utility for managing Sei blockchain daemon. `seictl` provides tools to patch and modify Sei node
configuration files (TOML) and genesis files (JSON) with ease. More features to be added in due course.

## Features

- **Configuration Management**: Patch Sei daemon configuration files (`app.toml`, `client.toml`, `config.toml`)
- **Genesis Management**: Apply merge patches to genesis JSON files
- **Universal Patching**: Apply merge patches to any TOML or JSON file
- **Smart Target Detection**: Automatically detects which configuration file to modify based on patch content
- **Flexible Output**: Write to stdout, a specific file, or modify files in-place
- **Atomic Writes**: Safe file modifications using atomic write operations
- **Merge Patch Algorithm**: Intelligently merges patches with existing configurations

## Installation

### Pre-built Binaries (Recommended)

Pre-built binaries are available for Linux, macOS, and Windows. Download the latest release from
the [releases page](https://github.com/sei-protocol/seictl/releases).

#### Quick Install

<details>
<summary><b>Linux (x86_64)</b></summary>

```bash
curl -LO https://github.com/sei-protocol/seictl/releases/latest/download/seictl_Linux_x86_64.tar.gz
tar -xzf seictl_Linux_x86_64.tar.gz
sudo mv seictl /usr/local/bin/
```
</details>

<details>
<summary><b>Linux (ARM64)</b></summary>

```bash
curl -LO https://github.com/sei-protocol/seictl/releases/latest/download/seictl_Linux_arm64.tar.gz
tar -xzf seictl_Linux_arm64.tar.gz
sudo mv seictl /usr/local/bin/
```
</details>

<details>
<summary><b>Linux (ARMv7)</b></summary>

```bash
curl -LO https://github.com/sei-protocol/seictl/releases/latest/download/seictl_Linux_armv7.tar.gz
tar -xzf seictl_Linux_armv7.tar.gz
sudo mv seictl /usr/local/bin/
```
</details>

<details>
<summary><b>macOS (Apple Silicon)</b></summary>

```bash
curl -LO https://github.com/sei-protocol/seictl/releases/latest/download/seictl_Darwin_arm64.tar.gz
tar -xzf seictl_Darwin_arm64.tar.gz
sudo mv seictl /usr/local/bin/
```
</details>

<details>
<summary><b>macOS (Intel)</b></summary>

```bash
curl -LO https://github.com/sei-protocol/seictl/releases/latest/download/seictl_Darwin_x86_64.tar.gz
tar -xzf seictl_Darwin_x86_64.tar.gz
sudo mv seictl /usr/local/bin/
```
</details>

<details>
<summary><b>Windows (x86_64)</b></summary>

```powershell
# Download from: https://github.com/sei-protocol/seictl/releases/latest/download/seictl_Windows_x86_64.zip
# Extract and add to PATH
```
</details>

#### Verify Installation

```bash
seictl --version
```

#### Verify Download (Optional)

All releases include a `checksums.txt` file for verification, e.g.:

```bash
# Download checksums
curl -LO https://github.com/sei-protocol/seictl/releases/latest/download/checksums.txt

# Verify (Linux/macOS)
sha256sum -c checksums.txt 2>&1 | grep seictl_Linux_x86_64.tar.gz
```

### Build from Source

If you prefer to build from source or need a specific configuration:

#### Prerequisites

- Go 1.25.4 or higher

#### Build

```bash
git clone https://github.com/sei-protocol/seictl.git
cd seictl
make build
```

Or directly with `go`:

```bash
go build -o build/seictl .
```

### Install via Go

```bash
go install github.com/sei-protocol/seictl@latest
```

Or from a local clone:

```bash
make install
```

## Usage

```
seictl [global options] command [command options] [arguments...]
```

### Global Options

- `--home <path>`: Sei home directory (default: `~/.sei`, can be set via `SEI_HOME` environment variable)

## Commands

### Patch Command

#### `patch`

Apply a merge-patch to any TOML or JSON file. This is a universal patching command that works with any file format, not
just Sei-specific configurations.

```bash
seictl patch --target <file-path> [patch-file]
```

**Options:**

- `--target <path>`: Path to the TOML or JSON file to patch (required)
- `-o, --output <path>`: Write output to specified file
- `-i, --in-place-rewrite`: Modify the target file in-place

**Examples:**

```bash
# Patch any TOML file
seictl patch --target /path/to/config.toml patch.toml

# Patch any JSON file from stdin
echo '{"new_key": "value"}' | seictl patch --target /path/to/data.json

# Patch and save to a new file
seictl patch --target myconfig.toml patch.toml -o modified.toml

# Patch and modify in-place
seictl patch --target settings.json patch.json -i
```

**Note:** The file extension (`.toml` or `.json`) is used to determine the format automatically.

### Genesis Commands

#### `genesis patch`

Apply a merge-patch to the Sei genesis JSON file.

```bash
seictl genesis patch [patch-file]
```

**Options:**

- `-o, --output <path>`: Write output to specified file
- `-i, --in-place-rewrite`: Modify the genesis file in-place

**Examples:**

```bash
# Patch from file and output to stdout
seictl genesis patch patch.json

# Patch from stdin
echo '{"chain_id": "sei-testnet"}' | seictl genesis patch

# Patch and save to a new file
seictl genesis patch patch.json -o genesis-modified.json

# Patch and modify the original file in-place
seictl genesis patch patch.json -i
```

### Config Commands

#### `config patch`

Apply a merge-patch to a Sei configuration TOML file.

```bash
seictl config [--target <app|client|config>] patch [patch-file]
```

**Options:**

- `--target <type>`: Specify which configuration file to patch (`app`, `client`, or `config`)
    - If not specified, the target is automatically detected based on the patch content
- `-o, --output <path>`: Write output to specified file
- `-i, --in-place-rewrite`: Modify the configuration file in-place

**Examples:**

```bash
# Patch with auto-detection
seictl config patch patch.toml

# Explicitly specify the target config
seictl config --target app patch patch.toml

# Patch from stdin and output to stdout
echo 'minimum-gas-prices = "0.01usei"' | seictl config patch

# Patch and modify in-place
seictl config --target app patch patch.toml -i

# Patch and save to specific location
seictl config patch patch.toml -o /path/to/output.toml
```

## Configuration Targets

The `config` command can work with three different configuration files:

### `app.toml`

Application-level configuration including:

- Gas prices and block settings
- State management (state-sync, state-commit, state-store)
- EVM configuration
- Telemetry and monitoring
- API, gRPC, and Rosetta endpoints
- IAVL and WASM settings

### `client.toml`

Client-level configuration including:

- Chain ID
- Keyring backend
- Output format
- Node endpoint
- Broadcast mode

### `config.toml`

Node-level configuration including:

- Proxy app and database settings
- Logging configuration
- RPC and P2P settings
- Mempool and consensus parameters
- State sync and block sync
- Transaction indexing

## Merge Patch Behavior

The merge patch algorithm works as follows:

1. **Nested merging**: Patches are merged recursively into nested structures
2. **Null deletion**: Setting a value to `null` removes that key from the configuration
3. **Addition**: New keys in the patch are added to the configuration
4. **Replacement**: Existing scalar values are replaced with patch values

**Example:**

Original:

```toml
[api]
enable = true
address = "tcp://0.0.0.0:1317"
```

Patch:

```toml
[api]
address = "tcp://0.0.0.0:1318"
swagger = true
```

Result:

```toml
[api]
enable = true
address = "tcp://0.0.0.0:1318"
swagger = true
```

## Examples

<details>
<summary><b>Update Minimum Gas Prices</b></summary>

```bash
echo 'minimum-gas-prices = "0.02usei"' | seictl config patch -i
```
</details>

<details>
<summary><b>Enable API Endpoint</b></summary>

```bash
cat > patch.toml << EOF
[api]
enable = true
address = "tcp://0.0.0.0:1317"
EOF

seictl config --target app patch patch.toml -i
```
</details>

<details>
<summary><b>Modify Genesis Chain ID</b></summary>

```bash
echo '{"chain_id": "sei-mainnet-1"}' | seictl genesis patch -i
```
</details>

<details>
<summary><b>Update Multiple Configuration Sections</b></summary>

```bash
cat > patch.toml << EOF
minimum-gas-prices = "0.01usei"

[telemetry]
enabled = true
prometheus-retention-time = 60

[api]
enable = true
swagger = true
EOF

seictl config patch patch.toml -i
```
</details>

<details>
<summary><b>Patch a Custom TOML Configuration</b></summary>

```bash
# Patch any TOML file outside the Sei directory structure
cat > custom-patch.toml << EOF
[database]
host = "localhost"
port = 5432
EOF

seictl patch --target /etc/myapp/config.toml custom-patch.toml -i
```
</details>

<details>
<summary><b>Patch a Custom JSON Data File</b></summary>

```bash
# Modify any JSON file
echo '{"version": "2.0", "debug": true}' | seictl patch --target /path/to/settings.json -i
```
</details>

<details>
<summary><b>Using Custom Sei Home Directory</b></summary>

```bash
# Via environment variable
export SEI_HOME=/custom/path/.sei
seictl config patch patch.toml

# Via flag
seictl --home /custom/path/.sei config patch patch.toml
```
</details>

## Command Comparison

### When to Use Each Command

- **`patch`**: Use for patching any arbitrary TOML or JSON file on your system. Requires explicit `--target` path.
- **`genesis patch`**: Use specifically for Sei genesis files. Automatically uses `$HOME/.sei/config/genesis.json`.
- **`config patch`**: Use specifically for Sei configuration files with automatic target detection. Automatically uses
  files in `$HOME/.sei/config/`.

## File Locations

By default, `seictl` looks for configuration files in the following locations:

- Genesis: `$HOME/.sei/config/genesis.json`
- App config: `$HOME/.sei/config/app.toml`
- Client config: `$HOME/.sei/config/client.toml`
- Node config: `$HOME/.sei/config/config.toml`

Where `$HOME` is the value of the `--home` flag or the `SEI_HOME` environment variable.

## Safety Features

- **Atomic Writes**: All file modifications use atomic write operations (write to temp file, then rename)
- **Permission Preservation**: In-place modifications preserve original file permissions
- **Format Validation**: Validates file extensions before processing (must be `.toml` or `.json`)
- **Target Validation**: Prevents accidental modification of wrong configuration files
- **Auto-detection Safety**: Refuses to proceed if patch could apply to multiple targets
- **Early Validation**: Checks file format and existence before reading patch data

## License

This project is licensed under the Apache License 2.0. See the [LICENSE](LICENSE.md) file for details.