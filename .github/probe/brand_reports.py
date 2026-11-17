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

def host(url):
    try:
        return urllib.parse.urlparse(url or "").netloc
    except Exception:
        return ""

def rows(kind, offset, n, tag, wide):
    try:
        data = get({"brand_name": "anthropic", "kind": kind, "sort": "recent", "n": n, "offset": offset, "full_data": "false"})
    except Exception as e:
        print("BR2 %s offset=%s error=%s" % (kind, offset, str(e)[:160]))
        return
    reports = data.get("reports") or []
    print("BR2META %s offset=%s returned=%d has_more=%s api_offset=%s" % (kind, offset, len(reports), data.get("has_more"), data.get("offset")))
    for i, r in enumerate(reports):
        rep = r.get("report") or {}
        a = (r.get("analysis") or [{}])[0]
        ev = r.get("evidence") or {}
        when = ev.get("observed_at") or ev.get("source_published_at") or ev.get("ingested_at") or ""
        if i == 0:
            print("BR2REC %s recurrence=%s" % (kind, short(json.dumps(r.get("recurrence")), 400)))
        print("%s %s | %s | sev=%.2f | %s/%s | %s %s | T: %s | I: %s | X: %s" % (
            tag, rep.get("public_id"), short(when, 10), float(a.get("severity_level") or 0),
            ev.get("entity_relationship"), ev.get("intake_method"), ev.get("source_platform"), host(ev.get("source_url")),
            short(a.get("title"), 130), short(ev.get("reported_impact"), 130 if wide else 0), short(ev.get("excerpt"), 230 if wide else 170)))

rows("firsthand", 50, 50, "FH", True)
rows("firsthand", 100, 50, "FH", True)
for off in (0, 1200, 2400, 3600, 4800):
    rows("unknown", off, 40, "UN", False)
for off in (0, 1000):
    rows("coverage", off, 25, "CV", False)
