# CleanApp physical evaluation preview

Read-only buyer preview intended for **https://cleanapp.io/grepped/**. This app reads only one frozen physical-500 evaluation ZIP. It has no database credentials, remote-data client, digital-preview inputs, review persistence, upload facility, or bulk ZIP endpoint. It does not alter or decode originals on the server.

The server hashes the archive with a streaming file digest before loading metadata. It retains that descriptor so pathname replacement cannot redirect media reads; before serving an original it checks the archive's size/change timestamps and verifies the member's byte length and SHA-256 against the pinned record. Media is returned from those exact verified bytes. The input archive is never extracted.

## Run locally behind the HTTPS reverse proxy

Use a dedicated environment with `pip install -r requirements.txt`. The pins match a tested Python 3.14 environment. For tests, install `requirements-test.txt` and run `python -m unittest discover -s tests -v` from this directory.

Required server environment:

- `GREPPED_ZIP`: absolute path to the approved frozen ZIP, mounted read-only.
- `GREPPED_ZIP_SHA256`: exact expected archive SHA-256 from the approved `.zip.sha256` file.
- `GREPPED_PASSWORD_HASH`: PBKDF2 hash generated below. No plaintext password is embedded in the app, HTML, JavaScript, Dockerfile, or documentation.

Optional environment:

- `GREPPED_PUBLIC_ORIGIN=https://cleanapp.io` is the canonical HTTPS origin. Host and mutation Origin must match exactly. Redirect `www` preview requests to this canonical origin in nginx.
- `GREPPED_TRUSTED_PROXY_IPS`: comma-separated **exact socket-peer IPs** of your dedicated proxy, only if per-client throttling behind it is needed. Such a peer must overwrite `X-Real-IP` with its actual client address. The app ignores all forwarding headers from other peers and never trusts `X-Forwarded-For`. Without this setting, all clients behind a proxy share its throttle bucket.
- `GREPPED_PORT=8790` applies only to `python server.py`, which binds loopback. The Docker factory below binds port 8000 inside its isolated container.

Generate the server-side password hash in the operator terminal; the password is entered using a hidden prompt:

```sh
python server.py --hash-password
```

Protect the environment file with mode `0600`. Do not paste secrets in logs or command arguments. The dollar signs in the stored hash must remain literal; Docker `--env-file` preserves them. Compose interpolation needs appropriate escaping if used instead.

The standalone Dockerfile accepts no dataset or secret at build time and its build context includes only code, static assets and pinned requirements. Example operator commands after setting an appropriate environment file:

```sh
docker build -t cleanapp-grepped-preview .
docker run --name cleanapp-grepped-preview --restart unless-stopped \
  --read-only --cap-drop ALL --security-opt no-new-privileges \
  --memory 512m --pids-limit 64 \
  -p 127.0.0.1:8791:8000 --env-file /opt/cleanapp/grepped-preview/preview.env \
  -v /opt/cleanapp/grepped-preview/data:/data:ro cleanapp-grepped-preview
```

The process runs as UID 10001; ensure the mounted ZIP and its parent directory are readable/searchable by this UID. The file need not be writable. No database Docker network should be attached. Runtime startup command is `uvicorn server:create_from_env --factory --host 0.0.0.0 --port 8000 --no-access-log --no-proxy-headers`. Use one worker: sessions and throttles are process-local. A restart invalidates all sessions. TLS and public reachability belong to the host nginx; preserve the `/grepped` URI in `proxy_pass` and overwrite `Host` and `X-Real-IP`. Expose only `/grepped` and `/grepped/…`, with a `www` → apex redirect. Do not alias or serve the ZIP/data directory in nginx.

## Auth and routes

Only GET/POST `/grepped/login` is public. All data, media, static assets, unknown routes, and any health-like path require a valid session. The app disables OpenAPI/docs and has no public health endpoint; use a TCP/process check for container readiness. Correct login sets a random opaque host-only cookie with `HttpOnly; Secure; SameSite=Strict; Path=/grepped`. Only a SHA-256 token digest is held in process memory. Session TTL is two hours. Same-origin POST logout revokes it. Login allows five attempts per client within a conservative 15-minute window and 30 global attempts within five minutes. Attempts include successful logins. State is bounded; login failures reveal no dataset information. HTTPS is required for browser cookies.

Authenticated GET routes are `/grepped/`, `/grepped/api/items`, `/grepped/api/summary`, `/grepped/media/CA-0001` (and other exact bundle IDs), and the two explicit `/grepped/static/…` assets. Media opens an individual original inline. The app has no general static mount, arbitrary ZIP-member route, directory listing, JSON mutation or review API. Login/logout are the only POST routes. Responses are `no-store`, with CSP, no-referrer, frame denial, nosniff and noindex headers.

## Public projection and limits

The public record is an explicit allowlist: opaque package ID, selected saved caption/source, text-derived category/basis, report text if supplied in the approved ZIP, report timestamp with basis, pixel dimensions/file size/MIME/SHA, annotation status/model revision if recorded, and source-labeled existing annotation fields. The UI does not turn stored probabilities or validity fields into certified labels. It renders stored text with `textContent`, never HTML.

Internal report IDs, all structured coordinates (including the ZIP's coarse location), private provenance, internal source paths, owner-review status, review notes/votes, raw manifest, arbitrary archive members and digital inputs are excluded. Corpus counts come only from the ZIP inventory's four allowlisted report-record fields, with its snapshot date and gross-inventory qualification; website cache counts are excluded.

This service expects an independently approved archive and an operator-supplied archive digest. Existing textual annotation content and original JPEG embedded metadata are preserved, including any residual identifiers in those approved originals/text. A strict structural projection is not a new content, privacy or rights review. Categories and captions remain generated analysis, timestamps are not verified capture times, and the 500 are a curated showcase rather than a statistical sample. No new vision, transformation, compression, annotations or rights certification occurs here. The app currently serves the 500 physical images only; the separate native-digital preview must remain local.

Deployment, production nginx changes, outgoing messages, and data release require a separate operator action. This directory's build/tests perform none of them.
