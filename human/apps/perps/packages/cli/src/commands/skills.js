module.exports = {
  command: "skills <subcommand>",
  describe:
    "Install paxeer-network agent skills for plugin workflows (create, write, add, submit)",
  builder: (yargs) => {
    return yargs
      .commandDir("skills")
      .demandCommand(1, "Please provide a skills subcommand.");
  },
};
