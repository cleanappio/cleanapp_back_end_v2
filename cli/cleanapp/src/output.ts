import { CLIError, EXIT } from "./errors.js";
import type { RuntimeConfig } from "./runtime.js";

export function printJson(obj: unknown): void {
  // stdout only
  // eslint-disable-next-line no-console
  console.log(JSON.stringify(obj, null, 2));
}

export function printHuman(text: string): void {
  // eslint-disable-next-line no-console
  console.log(text.trimEnd());
}

export function requireToken(cfg: RuntimeConfig): void {
  if (cfg.token) return;
  throw new CLIError("Missing token. Set CLEANAPP_API_TOKEN or run `cleanapp init` to store a token locally.", EXIT.USER);
}

