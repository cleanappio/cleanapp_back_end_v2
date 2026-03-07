import { CLI_VERSION } from "./version.js";

export type LegacyIngestItem = {
  source_id?: string;
  sourceId?: string;
  title?: string;
  description?: string;
  desc?: string;
  lat?: number;
  lng?: number;
  collected_at?: string;
  collectedAt?: string;
  agent_id?: string;
  agentId?: string;
  agent_version?: string;
  agentVersion?: string;
  source_type?: string;
  sourceType?: string;
  media?: Array<{ url?: string; sha256?: string; content_type?: string; contentType?: string }>;
  [key: string]: unknown;
};

export function isWireSubmission(value: any): boolean {
  return Boolean(
    value &&
      typeof value === "object" &&
      typeof value.schema_version === "string" &&
      value.agent &&
      typeof value.agent === "object" &&
      value.report &&
      typeof value.report === "object",
  );
}

export function toWireSubmission(item: LegacyIngestItem, defaults?: { agentId?: string; agentVersion?: string }): Record<string, unknown> {
  if (isWireSubmission(item)) return item as Record<string, unknown>;

  const sourceId = String(item.source_id || item.sourceId || "").trim();
  const title = String(item.title || "").trim();
  const description = String(item.description || item.desc || "").trim();
  const collectedAt = String(item.collected_at || item.collectedAt || "").trim();
  const agentId = String(item.agent_id || item.agentId || defaults?.agentId || "cleanapp-cli").trim();
  const agentVersion = String(item.agent_version || item.agentVersion || defaults?.agentVersion || CLI_VERSION).trim();
  const sourceType = String(item.source_type || item.sourceType || "").trim() || "text";

  const hasLocation = Number.isFinite(item.lat) && Number.isFinite(item.lng);
  const domain = hasLocation ? "physical" : "digital";
  const problemType =
    sourceType === "vision"
      ? "vision_observation"
      : sourceType === "sensor"
        ? "sensor_observation"
        : sourceType === "web"
          ? "web_report"
          : "general_issue";

  const evidenceBundle = Array.isArray(item.media)
    ? item.media
        .filter((m) => m && (m.url || m.sha256))
        .map((m, idx) => ({
          evidence_id: `${sourceId || "wire"}_ev_${idx + 1}`,
          type: "media",
          uri: m.url || undefined,
          sha256: m.sha256 || undefined,
          mime_type: m.content_type || m.contentType || undefined,
          captured_at: collectedAt || undefined,
        }))
    : [];

  return {
    schema_version: "cleanapp-wire.v1",
    source_id: sourceId,
    submitted_at: new Date().toISOString(),
    observed_at: collectedAt || undefined,
    agent: {
      agent_id: agentId,
      agent_name: "CleanApp CLI",
      agent_type: "cli",
      operator_type: "human",
      auth_method: "api_key",
      software_version: agentVersion,
      execution_mode: "interactive",
    },
    provenance: {
      generation_method: "cleanapp_cli",
      chain_of_custody: ["cleanapp", "wire"],
    },
    report: {
      domain,
      problem_type: problemType,
      title,
      description,
      confidence: 0.7,
      location: hasLocation
        ? {
            kind: "coordinate",
            lat: Number(item.lat),
            lng: Number(item.lng),
            place_confidence: 0.7,
          }
        : undefined,
      digital_context: hasLocation
        ? undefined
        : {
            submitted_via: "cleanapp-cli",
            source_type: sourceType,
          },
      evidence_bundle: evidenceBundle.length > 0 ? evidenceBundle : undefined,
    },
    delivery: {
      requested_lane: "auto",
    },
  };
}
