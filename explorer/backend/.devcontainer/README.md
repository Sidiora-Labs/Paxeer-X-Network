# Explorer Backend Development with VSCode Devcontainers

## Table of Contents
1. [Motivation](#motivation)
2. [Setting Up VSCode Devcontainer Locally](#setting-up-vscode-devcontainer-locally)
3. [Configuring Postgres DB Access](#configuring-postgres-db-access)
4. [Developing the Explorer Backend](#developing-the-explorer-backend)
5. [Upgrading Elixir Version](#upgrading-elixir-version)
6. [Contributing](#contributing)

## Motivation

Setting up a local development environment for the Blockscout-based explorer backend can be time-consuming and error-prone. This devcontainer setup streamlines the process by providing a pre-configured environment with all necessary dependencies. It ensures consistency across development environments, reduces setup time, and allows developers to focus on coding rather than configuration.

Key benefits include:
- Pre-configured environment with Elixir, Phoenix, and Node.js
- Integrated PostgreSQL database
- Essential VS Code extensions pre-installed
- Simplified database management
- Consistent development environment across team members

## Setting Up VSCode Devcontainer Locally

1. Clone the Paxeer X Network repository and change into the explorer backend, the directory that holds this `.devcontainer`:
   ```
   git clone https://github.com/Sidiora-Labs/Paxeer-X-Network.git
   cd Paxeer-X-Network/explorer/backend
   ```

2. Open the backend in VS Code:
   ```
   code .
   ```

3. Before re-opening in the container, you may find it useful to configure SSH authorization. To do this:
   
   a. Ensure you have SSH access to GitHub configured on your local machine.
   
   b. Open `.devcontainer/devcontainer.json`.
   
   c. Uncomment the `mounts` section:
      ```json
      "mounts": [
        "source=${localEnv:HOME}/.ssh/config,target=/home/vscode/.ssh/config,type=bind,consistency=cached",
        "source=${localEnv:HOME}/.ssh/id_rsa,target=/home/vscode/.ssh/id_rsa,type=bind,consistency=cached"
      ],
      ```
   
   d. Adjust the paths if your SSH keys are stored in a different location.

   e. Use `git update-index --assume-unchanged .devcontainer/devcontainer.json` to prevent the changes to `devcontainer.json` from appearing in `git status` and VS Code's Source Control. To undo the changes, use `git update-index --no-assume-unchanged .devcontainer/devcontainer.json`.

4. When prompted, click "Reopen in Container". If not prompted, press `F1`, type "Remote-Containers: Reopen in Container", and press Enter.

5. VS Code will build the devcontainer. This process includes:
   - Pulling the base Docker image
   - Installing specified VS Code extensions
   - Setting up the PostgreSQL database
   - Installing project dependencies
   
   This may take several minutes the first time.

6. Once the devcontainer is built, you'll be working inside the containerized environment.

7. If you modified the `devcontainer.json` file in step 3, you may want to execute `git update-index --assume-unchanged .devcontainer/devcontainer.json` in a terminal within your devcontainer to prevent the changes to `devcontainer.json` from appearing in `git status` and VS Code's Source Control.

### Additional Setup for Cursor.ai Users

If you're using Cursor.ai instead of VSCode, you may need to perform some additional setup steps. Please note that these changes will not persist after reloading the devcontainer, so you may need to repeat these steps each time you start a new session.

1. **Git Configuration**: You may encounter issues when trying to perform Git operations from the terminal or the "Source Control" tab. To resolve this, set up your Git configuration inside the devcontainer:

   a. Open a terminal in your devcontainer.
   b. Set your Git username:
      ```
      git config --global user.name "Your Name"
      ```
   c. Set your Git email:
      ```
      git config --global user.email "your.email@example.com"
      ```

   Replace "Your Name" and "your.email@example.com" with your actual name and email associated with your GitHub account.

2. **ElixirLS: Elixir support and debugger** (JakeBecker.elixir-ls): This extension may not be automatically installed in Cursor.ai, even though it's specified in the devcontainer configuration. To install it manually:

   a. Open the Extensions tab.
   b. Search for "JakeBecker.elixir-ls".
   c. Look for the extension "ElixirLS: Elixir support and debugger" by JakeBecker and click "Install".

Remember, you may need to repeat these steps each time you start a new Cursor.ai session with the devcontainer.

### Signing in to GitHub for Pull Request Extension

1. In the devcontainer, click on the GitHub icon in the Primary sidebar.
2. Click on "Sign in to GitHub" and follow the prompts to authenticate.

## Configuring Postgres DB Access

To configure access to the PostgreSQL database using the VS Code extension:

1. Click on the PostgreSQL icon in the Primary sidebar.
2. Click "+" (Add Connection) in the PostgreSQL explorer.
3. Use the following details:
   - Host: `db`
   - User: `postgres`
   - Password: `postgres`
   - Port: `5432`
   - Use an ssl connection: "Standard connection"
   - Database: `app`
   - The display name: "<some name>"

These credentials are derived from the `DATABASE_URL` in the `bs` script.

## Developing the Explorer Backend

### Configuration

Before running the Blockscout server, you need to set up the configuration:

1. Create `.devcontainer/.blockscout_config`; the `bs` script loads it when it is present. Upstream's `.blockscout_config.example` is not tracked in this repository.
2. Put the environment variables your development setup needs in it, for example `CHAIN_TYPE=paxeer_x` and `ETHEREUM_JSONRPC_VARIANT=paxeer_x`.

For a comprehensive list of environment variables that can be set in this configuration file, refer to the [Blockscout documentation](https://docs.blockscout.com/setup/env-variables).

### Using the `bs` Script

The `bs` script in `.devcontainer/bin/` helps orchestrate common development tasks. Here are some key commands:

- Initialize the project: `bs --init`
- Initialize or re-initialize the database: `bs --db-init` (This will remove all data and tables from the DB and re-create the tables)
- Run the server: `bs`
- Run the server without syncing: `bs --no-sync`
- Recompile the project: `bs --recompile` (Use this when new dependencies arrive after a merge or when switching to another `CHAIN_TYPE`)
- Run various checks: `bs --spellcheck`, `bs --dialyzer`, `bs --credo`, `bs --format`

For a full list of options, run `bs --help`.

### Interacting with the Blockscout API

For local devcontainer setups, you can use API testing tools like Postman or Insomnia on your host machine to interact with the Blockscout API running in the container:

1. Ensure the Blockscout server is running in the devcontainer.
2. In the API testing tool on your host machine, use `http://localhost:4000` as the base URL.
3. Example endpoint: `GET http://localhost:4000/api/v2/blocks`

This allows testing API endpoints directly from your host machine while the server runs in the container.

### Troubleshooting

If you face issues with dependency compilation or dialyzer after container creation:

1. Check for untracked files: `git ls-files --others`
2. Remove compilation artifacts or generated files if present.
3. For persistent issues, consider cleaning all untracked files (use with caution):
   ```
   git clean -fdX
   bs --recompile
   ```

This ensures a clean compilation environment within the container.

## Upgrading Elixir Version

To upgrade the Elixir version:

1. Open `.devcontainer/Dockerfile`.
2. Update the `VARIANT` argument with the desired Elixir version.
3. Rebuild the devcontainer.

Note: Ensure that the version you choose is compatible with the project dependencies.

The devcontainer runs upstream's prebuilt `ghcr.io/blockscout/devcontainer-elixir` image named in `.devcontainer/docker-compose.yml`. To try a new Elixir version before such an image exists, uncomment the `build` section of that file so it builds from `.devcontainer/Dockerfile` instead, and change the image tag once upstream publishes a matching one.

## Contributing

When contributing changes that require additional checks for specific blockchain types:

1. Open `.devcontainer/bin/chain-specific-checks`.
2. Add your checks under the appropriate `CHAIN_TYPE` case.
3. Ensure your checks exit with a non-zero code if unsuccessful.

Remember to document any new checks or configuration options in this README.