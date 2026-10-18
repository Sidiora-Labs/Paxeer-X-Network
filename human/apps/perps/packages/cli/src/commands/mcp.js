module.exports = {
  command: "mcp <subcommand>",
  describe: "MCP related commands",
  builder: (yargs) => {
    return yargs
      .commandDir("mcp")
      .demandCommand(1, "Please provide an MCP subcommand.");
  },
};
