import crypto from "node:crypto";
import fs from "node:fs/promises";
import path from "node:path";

export function genSourceId(prefix = "cli"): string {
  return `${prefix}_${crypto.randomUUID()}`;
}

export async function readFileUtf8(p: string): Promise<string> {
  return await fs.readFile(p, "utf8");
}

export function fileExt(p: string): string {
  return path.extname(p || "").toLowerCase();
}

export function parseFloatArg(v: any, name: string): number {
  const n = typeof v === "number" ? v : Number(String(v));
  if (!Number.isFinite(n)) throw new Error(`Invalid ${name}: ${v}`);
  return n;
}

export function parseSinceToMs(s: string | undefined): number | undefined {
  if (!s) return undefined;
  const raw = String(s).trim().toLowerCase();
  const m = raw.match(/^(\d+)\s*(h|d)$/);
  if (!m) return undefined;
  const n = Number(m[1]);
  if (!Number.isFinite(n) || n <= 0) return undefined;
  const unit = m[2];
  if (unit === "h") return n * 60 * 60 * 1000;
  if (unit === "d") return n * 24 * 60 * 60 * 1000;
  return undefined;
}

