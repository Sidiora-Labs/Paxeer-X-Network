const {
  generateFromTemplate,
  toPascalCase,
  toKebabCase,
  toCamelCase,
  validateName,
} = require("../../internal/templateGenerator");
const {
  input,
  select,
  confirm,
  heading,
  info,
  success,
  error,
  warn,
} = require("../../shared");
const {
  CLI_BIN_NAME,
  INTERCEPTOR_TARGETS,
} = require("../../internal/constants");
const { generateManifest } = require("../../internal/manifest");
const {
  maybePrintPaxeerNetworkDevEnvironmentHints,
} = require("../../internal/paxeerNetworkSdkDocsMcpDetect");
const path = require("path");

const TEMPLATE_REPO = "PaxeerNetworkNetwork/paxeer-network-plugin-template";

/**
 * Validate npm package name for scoped and unscoped formats.
 * Keep this check intentionally strict to prevent publishing failures.
 * @param {string} name
 * @returns {boolean}
 */
function isValidNpmPackageName(name) {
  if (!name || typeof name !== "string") {
    return false;
  }
  const scoped = /^@[a-z0-9][a-z0-9-._]*\/[a-z0-9][a-z0-9-._]*$/;
  const unscoped = /^[a-z0-9][a-z0-9-._]*$/;
  return scoped.test(name) || unscoped.test(name);
}

/**
 * Build a npm-compliant package name before template rendering.
 * Prefer pluginId so users can control the final published package name.
 * @param {string} pluginId
 * @param {string} pluginName
 * @returns {string}
 */
function buildNpmName(pluginId, pluginName) {
  const candidate = typeof pluginId === "string" ? pluginId.trim() : "";
  if (isValidNpmPackageName(candidate)) {
    return candidate;
  }
  return toKebabCase(pluginName);
}

// Build template variables from user answers
function buildVars(pluginName, pluginId, interceptorTarget, npmName) {
  return {
    name: pluginName,
    pluginName,
    pluginId,
    npmName,
    pluginIdCamel: toCamelCase(pluginId),
    interceptorTarget,
    pluginVersion: "1.0.0",
    date: new Date().toISOString().split("T")[0],
  };
}

// Interactive prompts
async function promptPluginName(existing) {
  let name;
  let validation;
  do {
    name = await input("Enter plugin name:", existing || "");
    name = toPascalCase(name);
    validation = validateName(name);
    if (!validation.valid) {
      console.log(`  ${validation.error}`);
    }
  } while (!validation.valid);
  return name;
}

async function promptPluginId(pluginName, existing) {
  const defaultId = toKebabCase(pluginName);
  return await input("Enter plugin ID:", existing || defaultId);
}

async function promptInterceptorTarget() {
  return await select("Select interceptor target:", INTERCEPTOR_TARGETS, 0);
}

async function promptTargetDir(pluginName) {
  return await input("Enter target directory:", `./${pluginName}`);
}

// Main command
module.exports = {
  command: "plugin",
  describe: "Create a new plugin from template",
  builder: (yargs) => {
    return yargs
      .option("name", {
        alias: "n",
        type: "string",
        describe:
          "string; plugin name in PascalCase (input is normalized to PascalCase and validated). Required when `--no-interactive` is set",
        demandOption: false,
      })
      .option("id", {
        alias: "i",
        type: "string",
        describe:
          "string; plugin ID in kebab-case. If omitted in `--no-interactive` mode, derived from `--name` via toKebabCase()",
        demandOption: false,
      })
      .option("target", {
        alias: "t",
        type: "string",
        describe:
          "string; output directory. Must be empty or non-existent (template generator throws if non-empty). If omitted in `--no-interactive` mode, defaults to `./<PluginName>`",
        demandOption: false,
      })
      .option("interceptor", {
        type: "string",
        choices: INTERCEPTOR_TARGETS,
        describe:
          "string; interceptor target (must be one of the listed values). Required when `--no-interactive` is set",
        demandOption: false,
      })
      .option("skip-install", {
        type: "boolean",
        describe:
          "boolean; if true, skip `npm install` in the generated plugin folder",
        default: false,
      })
      .option("no-interactive", {
        type: "boolean",
        default: false,
        describe:
          "boolean; if true, do not prompt. Requires `--name` and `--interceptor`. If `--id`/`--target` are omitted, they default from `--name`. Prompts are also skipped when `--name --id --interceptor --target` are all provided",
      })
      .example(
        "paxeer-network-devkit create plugin --no-interactive --name MyPlugin --id my-plugin --interceptor Trading.Layout.Desktop --target ./my-plugin --skip-install",
        "Create plugin without prompts (explicit id/target) and skip npm install",
      )
      .example(
        "paxeer-network-devkit create plugin",
        "Interactive mode: prompt for name, id, interceptor, and target directory",
      );
  },
  handler: async (argv) => {
    heading("Create a New Plugin");

    // Determine if all required params are provided (non-interactive mode)
    const hasAllParams =
      argv.name && argv.id && argv.interceptor && argv.target;
    // yargs parses `--no-interactive` as boolean negation (`interactive: false`);
    // the declared "no-interactive" key itself never turns true.
    const nonInteractive =
      argv.interactive === false ||
      argv["no-interactive"] === true ||
      hasAllParams;

    if (!nonInteractive) {
      info(
        "This command will download a plugin template and customize it for you.\n",
      );
    }

    // Gather inputs (from flags or interactive prompts)
    let pluginName = argv.name ? toPascalCase(argv.name) : null;
    if (!pluginName) {
      if (nonInteractive) {
        error("Missing required option: --name");
        process.exitCode = 1;
        return;
      }
      pluginName = await promptPluginName("");
    }

    let pluginId = argv.id || null;
    if (!pluginId) {
      if (nonInteractive) {
        pluginId = toKebabCase(pluginName);
      } else {
        pluginId = await promptPluginId(pluginName, "");
      }
    }

    let interceptorTarget = argv.interceptor || null;
    if (!interceptorTarget) {
      if (nonInteractive) {
        error("Missing required option: --interceptor");
        process.exitCode = 1;
        return;
      }
      interceptorTarget = await promptInterceptorTarget();
    }

    let targetDir = argv.target || null;
    if (!targetDir) {
      if (nonInteractive) {
        targetDir = `./${pluginName}`;
      } else {
        targetDir = await promptTargetDir(pluginName);
      }
    }

    const skipInstall = argv["skip-install"];

    // Summary
    console.log("\n--- Plugin Creation Summary ---");
    console.log(`Plugin Name:       ${pluginName}`);
    console.log(`Plugin ID:         ${pluginId}`);
    console.log(`Interceptor Target: ${interceptorTarget}`);
    console.log(`Target Directory:  ${targetDir}`);
    console.log(`-----------------------------\n`);

    if (!nonInteractive) {
      const proceed = await confirm("Proceed with creation?");
      if (!proceed) {
        info("Cancelled.");
        return;
      }
    }

    // Build npm-safe package name up front and pass into template variables.
    const npmName = buildNpmName(pluginId, pluginName);
    if (npmName !== pluginId) {
      warn(
        `Plugin ID "${pluginId}" is not npm-compliant, using "${npmName}" as package name.`,
      );
    }

    // Build vars and generate
    const vars = buildVars(pluginName, pluginId, interceptorTarget, npmName);

    try {
      await generateFromTemplate({
        repo: TEMPLATE_REPO,
        targetDir: path.resolve(targetDir),
        vars,
        skipInstall,
      });

      // Generate paxeer-network manifest file
      const resolvedDir = path.resolve(targetDir);
      try {
        generateManifest(resolvedDir, {
          pluginId: pluginId,
          name: pluginName,
          tags: [],
        });
        info(
          `Manifest file generated at ${resolvedDir}/.paxeer-network-manifest.json`,
        );
      } catch (manifestErr) {
        warn(`Could not generate manifest: ${manifestErr.message}`);
      }

      success(`\nPlugin created successfully!`);
      info(`Next steps:`);
      info(`  cd ${targetDir}`);
      info(`  npm run dev  # or your setup command`);
      info(
        `  Fill in description, tags, and coverImages in .paxeer-network-manifest.json before '${CLI_BIN_NAME} submit'`,
      );
      maybePrintPaxeerNetworkDevEnvironmentHints(resolvedDir);
    } catch (err) {
      error(`Failed to create plugin: ${err.message}`);
      if (err.stack) {
        console.error(err.stack);
      }
      process.exitCode = 1;
    }
  },
};
