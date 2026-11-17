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

for brand in ["google ads", "googleads", "google-ads", "google_ads", "adwords"]:
    for kind in ["firsthand", "all"]:
        try:
            d = get({"brand_name": brand, "kind": kind, "sort": "recent", "n": 25, "full_data": "false"})
        except Exception as e:
            print("GA %s kind=%s error=%s" % (brand, kind, str(e)[:120]))
            continue
        reports = d.get("reports") or []
        print("GAMETA %s kind=%s returned=%d kind_counts=%s scope=%s" % (brand, kind, len(reports), json.dumps(d.get("kind_counts")), short(json.dumps(d.get("scope")), 200)))
        if kind != "firsthand" and (d.get("kind_counts") or {}).get("firsthand"):
            continue
        for r in reports[:25]:
            a = (r.get("analysis") or [{}])[0]
            ev = r.get("evidence") or {}
            rec = dict(r.get("recurrence") or {})
            print("GAROW %s | %s | %s | %s/%s/%s | %s | case=%s | T: %s | S: %s | I: %s | X: %s | upstream=%s resolution=%s" % (
                brand, (r.get("report") or {}).get("public_id"),
                short(ev.get("observed_at") or ev.get("source_published_at") or ev.get("ingested_at"), 10),
                ev.get("report_kind"), ev.get("entity_relationship"), ev.get("intake_method"), ev.get("source_platform"),
                rec.get("case_id"), short(a.get("title"), 160), short(a.get("summary"), 700),
                short(ev.get("reported_impact"), 300), short(ev.get("excerpt"), 500),
                short(ev.get("upstream_status"), 120), short(ev.get("resolution_status"), 120)))
