# Read-only: reads CleanApp's own public by-brand API and prints a compact
# listing of public reports. Changes nothing.
import json, urllib.parse, urllib.request

BASE = "https://live.cleanapp.io/api/v3/reports/by-brand"

def get(params):
    url = BASE + "?" + urllib.parse.urlencode(params)
    req = urllib.request.Request(url, headers={"User-Agent": "cleanapp-probe", "Accept": "application/json"})
    with urllib.request.urlopen(req, timeout=40) as resp:
        return json.loads(resp.read().decode("utf-8", "replace"))

def short(value, n):
    return " ".join(str(value or "").split())[:n]

# 1. What an archive-import row actually exposes.
data = get({"brand_name": "anthropic", "kind": "unknown", "sort": "recent", "n": 6, "offset": 2400, "full_data": "false"})
for r in (data.get("reports") or [])[:6]:
    a = (r.get("analysis") or [{}])[0]
    ev = r.get("evidence") or {}
    print("STUB %s | T: %s | S: %s | platform=%s intake=%s/%s label=%s validation=%s reason=%s why=%s" % (
        (r.get("report") or {}).get("public_id"), short(a.get("title"), 80), short(a.get("summary"), 400),
        ev.get("source_platform"), ev.get("intake_method"), ev.get("intake_channel"), short(ev.get("intake_label"), 60),
        ev.get("validation_status"), short(ev.get("classification_reason"), 160), short(ev.get("why_surfaced"), 160)))

# 2. How the system itself groups repeats: the recurrence block of known duplicates.
data = get({"brand_name": "anthropic", "kind": "firsthand", "sort": "recent", "n": 50, "offset": 0, "full_data": "false"})
for r in data.get("reports") or []:
    title = ((r.get("analysis") or [{}])[0].get("title") or "")
    if "TelemetrySafeError" in title or "Billing" in title or "Motion" in title:
        rec = dict(r.get("recurrence") or {})
        rec.pop("supporting_sources", None)
        print("RECUR %s | %s | %s" % ((r.get("report") or {}).get("public_id"), short(title, 70), short(json.dumps(rec), 420)))

# 3. Same composition check for the three biggest brands and two others.
for brand in ["apple", "google", "microsoft", "openai", "bluesky"]:
    try:
        d = get({"brand_name": brand, "kind": "all", "sort": "recent", "n": 1, "full_data": "false"})
        ov = d.get("overview") or {}
        print("COMP %s kind_counts=%s last30=%s last7=%s latest=%s" % (brand, json.dumps(d.get("kind_counts")), ov.get("reports_last_30_days"), ov.get("reports_last_7_days"), ov.get("latest_report_at")))
    except Exception as e:
        print("COMP %s error=%s" % (brand, str(e)[:120]))
