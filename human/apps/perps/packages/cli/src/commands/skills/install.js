const { spawnSync } = require("child_process");
const chalk = require("chalk");
const {
  PAXEER_NETWORK_SKILLS_REPO,
  PAXEER_NETWORK_PLUGIN_SKILL_NAMES,
} = require("../../internal/constants");

/**
 * Extra argv after `--` is forwarded verbatim to `skills add` (upstream flags).
 * @returns {string[]}
 */
function getPassthroughArgs() {
  const idx = process.argv.indexOf("--");
  if (idx === -1) {
    return [];
  }
  return process.argv.slice(idx + 1);
}

/**
 * @param {unknown} value
 * @returns {string[]}
 */
function asStringArray(value) {
  if (value == null) {
    return [];
  }
  if (Array.isArray(value)) {
    return value.map(String).filter(Boolean);
  }
  return [String(value)];
}

/**
 * Build argv for: npx -y skills add …
 * @param {Record<string, unknown>} argv
 * @returns {string[]}
 */
function buildSkillsAddArgs(argv) {
  const source =
    typeof argv.source === "string" && argv.source.trim() !== ""
      ? argv.source.trim()
      : PAXEER_NETWORK_SKILLS_REPO;

  const args = [source];
  const passthrough = getPassthroughArgs();

  if (argv.list) {
    args.push("--list");
    if (argv.yes === true) {
      args.push("-y");
    }
    appendSharedTail(args, argv);
    args.push(...passthrough);
    return args;
  }

  if (argv.all === true) {
    args.push("--all");
    appendSharedTail(args, argv);
    args.push(...passthrough);
    return args;
  }

  const userSkills = asStringArray(argv.skill);
  if (userSkills.length > 0) {
    for (const name of userSkills) {
      args.push("--skill", name);
    }
  } else {
    for (const name of PAXEER_NETWORK_PLUGIN_SKILL_NAMES) {
      args.push("--skill", name);
    }
  }
  args.push("-y");

  appendSharedTail(args, argv);
  args.push(...passthrough);
  return args;
}

/**
 * @param {string[]} args
 * @param {Record<string, unknown>} argv
 */
function appendSharedTail(args, argv) {
  if (argv.global === true) {
    args.push("-g");
  }
  const agents = asStringArray(argv.agent);
  for (const a of agents) {
    args.push("-a", a);
  }
  if (argv.copy === true) {
    args.push("--copy");
  }
}

/**
 * @param {string[]} skillsAddArgs
 * @returns {string[]}
 */
function buildNpxArgv(skillsAddArgs) {
  return ["-y", "skills", "add", ...skillsAddArgs];
}

/**
 * @param {string[]} argv
 */
function printDryRun(argv) {
  const line = ["npx", ...argv.map((a) => (/\s/.test(a) ? JSON.stringify(a) : a))].join(
    " ",
  );
  console.log(chalk.cyan("Dry run — would execute:"));
  console.log(line);
}

module.exports = {
  command: "install [source]",
  describe:
    "Install paxeer-network plugin workflow agent skills (defaults: official repo + four skills + -y)",
  builder: (yargs) => {
    return yargs
      .positional("source", {
        type: "string",
        describe:
          "GitHub owner/repo, URL, or local path (default: PaxeerNetworkNetwork/paxeer-network-skills)",
      })
      .option("list", {
        type: "boolean",
        default: false,
        describe: "List skills in the source repo without installing (no default --skill/-y)",
      })
      .option("skill", {
        alias: "s",
        type: "array",
        describe:
          "Install only these skill names (replaces default four when set; not used with --list)",
      })
      .option("all", {
        type: "boolean",
        default: false,
        describe:
          "Forward --all to skills CLI (install all skills/agents; skips default four + auto -y)",
      })
      .option("global", {
        alias: "g",
        type: "boolean",
        default: false,
        describe: "Install globally (forward -g to skills CLI)",
      })
      .option("agent", {
        alias: "a",
        type: "array",
        describe: "Target agent(s), e.g. -a cursor -a claude-code",
      })
      .option("copy", {
        type: "boolean",
        default: false,
        describe: "Copy files instead of symlinks (--copy)",
      })
      .option("yes", {
        alias: "y",
        type: "boolean",
        describe:
          "With --list only: also pass -y to skills CLI (default install already uses -y)",
      })
      .option("dry-run", {
        type: "boolean",
        default: false,
        describe: "Print the npx command without running it",
      })
      .example(
        "paxeer-network-devkit skills install",
        "Install all four paxeer-network plugin skills from the official repo (-y, non-interactive)",
      )
      .example(
        "paxeer-network-devkit skills install --list",
        "List available skills in the official repo",
      )
      .example(
        "paxeer-network-devkit skills install other/repo --skill my-skill -y",
        "Advanced: different repo and explicit skills (default four skipped)",
      )
      .example(
        "paxeer-network-devkit skills install -- --some-new-skills-flag",
        "Forward extra flags after -- to the skills CLI",
      );
  },
  handler: async (argv) => {
    const skillsAddArgs = buildSkillsAddArgs(argv);
    const npxArgs = buildNpxArgv(skillsAddArgs);

    if (argv["dry-run"]) {
      printDryRun(npxArgs);
      return;
    }

    const result = spawnSync("npx", npxArgs, {
      stdio: "inherit",
      shell: process.platform === "win32",
    });

    if (result.error) {
      throw result.error;
    }
    process.exitCode = result.status ?? 1;
  },
};
