import { Command } from "commander";

import { CLIError, EXIT } from "./errors.js";
import { CLI_VERSION } from "./version.js";
import { runInit } from "./commands/init.js";
import { addAuthWhoami } from "./commands/auth/whoami.js";
import { configGetCmd, configPathCmd, configSetCmd, logoutCmd } from "./commands/config/config.js";
import { addBulkSubmit } from "./commands/reports/bulk_submit.js";
import { addStatus } from "./commands/reports/status.js";
import { addSubmit } from "./commands/reports/submit.js";
import { addPresign } from "./commands/media/presign.js";
import { addMetrics } from "./commands/metrics.js";

function buildProgram(): Command {
  const program = new Command();

  program
    .name("cleanapp")
    .description("CleanApp CLI (thin wrapper around the CleanApp API)")
    .version(CLI_VERSION)
    .option("--api-url <url>", "Override API base URL (or use CLEANAPP_API_URL)")
    .option("--output <mode>", "Output mode: json|human (default json)", "json")
    .option("--human", "Alias for --output human", false)
    .option("--dry-run", "Print the HTTP request that would be sent and do not send it", false)
    .option("--trace", "Verbose HTTP tracing (redacts secrets; writes to stderr)", false);

  program
    .command("init")
    .description("Interactive first-time setup (writes ~/.cleanapp/config.json)")
    .action(async function () {
      await runInit(this);
    });

  // auth
  const auth = program.command("auth").description("Authentication helpers");
  addAuthWhoami(auth);

  // ingest/report commands
  addSubmit(program);
  addBulkSubmit(program);
  addStatus(program);

  // optional APIs
  addPresign(program);
  addMetrics(program);

  // config
  const config = program.command("config").description("Manage local CLI config (~/.cleanapp/config.json)");
  config
    .command("path")
    .description("Print resolved config path")
    .action(async function () {
      await configPathCmd(this);
    });

  config
    .command("get")
    .description("Get current config (token is redacted)")
    .argument("[key]", "Optional key: apiUrl|output|env|token")
    .action(async function (key?: string) {
      await configGetCmd(this, key);
    });

  config
    .command("set")
    .description("Set config key/value (token requires --token and masked input)")
    .argument("<key>", "apiUrl|output|env|token")
    .argument("[value]", "Value (not used for token)")
    .option("--token", "Explicitly allow setting token (prompts masked input)", false)
    .action(async function (key: string, value?: string) {
      await configSetCmd(this, key, value);
    });

  program
    .command("logout")
    .description("Remove token from local config (env vars unaffected)")
    .action(async function () {
      await logoutCmd(this);
    });

  return program;
}

export async function main(argv: string[] = process.argv): Promise<void> {
  const program = buildProgram();

  try {
    await program.parseAsync(argv);
  } catch (err: any) {
    if (err instanceof CLIError) throw err;
    // Commander sometimes throws plain Errors (e.g., invalid args). Classify as user error.
    const msg = err?.message ? String(err.message) : String(err);
    throw new CLIError(msg, EXIT.USER);
  }
}

