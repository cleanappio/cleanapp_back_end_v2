export function redactHeaderValue(k: string, v: string): string {
  const key = (k || "").toLowerCase();
  if (key === "authorization" || key.includes("token") || key.includes("secret") || key.includes("api-key")) {
    return "***redacted***";
  }
  return v;
}

export function redactHeaders(h: Record<string, string>): Record<string, string> {
  const out: Record<string, string> = {};
  for (const [k, v] of Object.entries(h || {})) out[k] = redactHeaderValue(k, v);
  return out;
}

export function redactBodySecrets(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(redactBodySecrets);
  if (value !== null && typeof value === "object") return Object.fromEntries(Object.entries(value).map(([key, item]) => [key, /token|secret|api.?key/i.test(key) ? "***redacted***" : redactBodySecrets(item)]));
  return value;
}

