import { randomUUID } from "node:crypto";
import fs from "node:fs/promises";

const uuidV4 = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;

export function reportHandling(opts: Record<string, any>): Record<string, string> | undefined {
  if (opts.publication === undefined && opts.notices === undefined) return undefined;
  const publication = String(opts.publication ?? "private");
  const notices = String(opts.notices ?? "none");
  if (!["private", "automatic"].includes(publication)) throw new Error("--publication must be private or automatic");
  if (!["none", "automatic"].includes(notices)) throw new Error("--notices must be none or automatic");
  return { publication, notices };
}

export function formatReportOutcome(receipt: any): string {
  const outcome = receipt?.outcome;
  if (!outcome) return "";
  const lines = [
    `analysis=${outcome.processing?.status ?? "unknown"} decision=${outcome.decision?.state ?? "pending"} final=${outcome.final === true}`,
    outcome.decision?.message ? `${outcome.decision.message}${outcome.decision.reason_code ? ` (${outcome.decision.reason_code})` : ""}` : "",
    `publication=${outcome.publication?.status ?? "unknown"}${outcome.publication?.reason_code ? ` (${outcome.publication.reason_code})` : ""}`,
    outcome.publication?.canonical_path ? `report=${outcome.publication.canonical_path}` : "",
  ];
  for (const delivery of Array.isArray(outcome.delivery) ? outcome.delivery : []) {
    lines.push(`${delivery.channel}=${delivery.status}${delivery.reason_code ? ` (${delivery.reason_code})` : ""} evidence=${delivery.evidence_count ?? 0}${delivery.last_sent_at ? ` last_sent=${delivery.last_sent_at}` : ""}`);
  }
  if (outcome.relationship?.type === "cluster_candidate") lines.push("Cluster candidate: this observation retains its own record and adds evidence to a related issue.");
  if (outcome.next_action?.type && outcome.next_action.type !== "none") lines.push(`next=${outcome.next_action.type}${outcome.next_action.fields?.length ? ` fields=${outcome.next_action.fields.join(",")}` : ""}`);
  if (outcome.final === true) lines.push("Final describes CleanApp's current decision and actions; it does not mean the issue is fixed.");
  return lines.filter(Boolean).join("\n");
}

export function toPublicAgentReport(opts: Record<string, any>): Record<string, unknown> {
  const description = String(opts.desc || "").trim();
  const sourceId = String(opts.sourceId || randomUUID()).trim().toLowerCase();
  if (!description || Buffer.byteLength(description, "utf8") > 8192) {
    throw new Error("public submit: --desc is required and must be at most 8192 UTF-8 bytes");
  }
  if (!uuidV4.test(sourceId)) throw new Error("public submit: --source-id must be a private random UUIDv4; reuse it only for retries of this report");
  if (Buffer.byteLength(String(opts.title || ""), "utf8") > 255) throw new Error("public submit: --title must be at most 255 UTF-8 bytes");
  const body: Record<string, unknown> = { description, source_id: sourceId, agent_name: "CleanApp CLI" };
  const handling = reportHandling(opts);
  if (handling) body.handling = handling;
  if (opts.title) body.title = opts.title;
  if ((opts.lat === undefined) !== (opts.lng === undefined)) throw new Error("public submit: --lat and --lng must be provided together");
  if (opts.lat !== undefined) {
    const lat = Number(opts.lat), lng = Number(opts.lng);
    if (!Number.isFinite(lat) || !Number.isFinite(lng) || Math.abs(lat) > 90 || Math.abs(lng) > 180) throw new Error("public submit: invalid coordinates");
    body.latitude = lat; body.longitude = lng;
  }
  if (opts.url) {
    const url = new URL(opts.url);
    if (!["http:", "https:"].includes(url.protocol) || url.username || url.password || Buffer.byteLength(String(opts.url), "utf8") > 2048) throw new Error("public submit: --url must be HTTP(S) without embedded credentials, at most 2048 UTF-8 bytes");
    body.url = opts.url;
  }
  if (opts.collectedAt) throw new Error("public submit: --collected-at requires an approved Wire credential");
  if (opts.sourceType && !["text", "web"].includes(opts.sourceType)) throw new Error("public submit: this source type requires approved Wire credentials; public intake accepts text and optional URL/coordinates");
  return body;
}

export async function writePrivateJson(file: string, value: unknown, replace = false): Promise<void> {
  const destination = replace ? `${file}.${randomUUID()}.tmp` : file;
  try {
    await fs.writeFile(destination, JSON.stringify(value, null, 2) + "\n", { flag: "wx", mode: 0o600 });
    if (replace) await fs.rename(destination, file);
  } catch (error) {
    if (replace) await fs.rm(destination, { force: true });
    throw error;
  }
}
