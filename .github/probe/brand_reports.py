# Read-only: reads CleanApp's own public by-brand API and prints a compact
# listing of public reports. Changes nothing.
import json, sys, urllib.parse, urllib.request

BASE = "https://live.cleanapp.io/api/v3/reports/by-brand"

def get(params):
    url = BASE + "?" + urllib.parse.urlencode(params)
    req = urllib.request.Request(url, headers={"User-Agent": "cleanapp-probe", "Accept": "application/json"})
    with urllib.request.urlopen(req, timeout=40) as resp:
        return json.loads(resp.read().decode("utf-8", "replace"))

def short(value, n):
    return " ".join(str(value or "").split())[:n]

for brand in ["anthropic", "claude"]:
    for kind in ["firsthand", "all"]:
        try:
            data = get({"brand_name": brand, "kind": kind, "sort": "recent", "n": 50, "full_data": "false"})
        except Exception as e:
            print("BR %s kind=%s error=%s" % (brand, kind, str(e)[:160]))
            continue
        reports = data.get("reports") or []
        meta = {k: v for k, v in data.items() if k != "reports"}
        print("BRMETA %s kind=%s returned=%d meta=%s" % (brand, kind, len(reports), short(json.dumps(meta), 900)))
        if reports and kind == "firsthand":
            print("BRKEYS %s report_keys=%s analysis_keys=%s evidence_keys=%s" % (
                brand, sorted(reports[0].keys()),
                sorted((reports[0].get("analysis") or [{}])[0].keys()),
                sorted((reports[0].get("evidence") or {}).keys())))
        if kind != "firsthand":
            continue
        for r in reports:
            rep = r.get("report") or {}
            a = (r.get("analysis") or [{}])[0]
            ev = r.get("evidence") or {}
            when = ev.get("observed_at") or ev.get("source_published_at") or ev.get("ingested_at") or rep.get("timestamp") or ""
            print("BRROW %s | %s | %s | sev=%s | %s/%s/%s | T: %s | S: %s | I: %s | X: %s" % (
                brand, rep.get("public_id"), short(when, 10), a.get("severity_level"),
                ev.get("report_kind"), ev.get("entity_relationship"), ev.get("intake_method"),
                short(a.get("title"), 140), short(a.get("summary") or a.get("description"), 260),
                short(ev.get("reported_impact"), 160), short(ev.get("excerpt"), 220)))
