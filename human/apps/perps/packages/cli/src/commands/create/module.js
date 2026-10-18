const { input, select, heading, info, success } = require("../../shared");
const { MODULE_TYPES } = require("../../internal/constants");

// create module command - default export for yargs commandDir
module.exports = {
  command: "module",
  describe: "Create a new module",
  builder: (yargs) => {
    return yargs
      .option("template", {
        alias: "t",
        type: "string",
        describe:
          "string; currently a placeholder and is not used by this command (module type is always prompted interactively)",
        demandOption: false,
      })
      .positional("name", {
        type: "string",
        describe:
          "string; module name used only for display in the guided flow (actual generation is implemented later)",
        default: "my-module",
      })
      .example(
        "paxeer-network-devkit create module",
        "Run guided module creation (prompt for module type)",
      )
      .example(
        "paxeer-network-devkit create module --name my-module",
        "Provide a module name (still prompts for module type)",
      );
  },
  handler: async (argv) => {
    heading("Create a New Module");
    info("This command will guide you through creating a new module.\n");

    // Module type: use argv or prompt
    const moduleType = await select("Select module type:", MODULE_TYPES, 0);

    console.log("\n--- Module Creation Details ---");
    console.log("Module name:", argv.name || "my-module");
    console.log("Module type:", moduleType);
    console.log("-----------------------------\n");

    success("Module creation flow completed!");
    success(`Module: ${argv.name || "my-module"}`);
    success(`Type: ${moduleType}`);
    console.log("\nNote: Actual file generation will be implemented later.");
  },
};
