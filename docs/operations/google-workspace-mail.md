# Google Workspace outgoing mail

The active report notification service (`email-service`) and password reset service
(`auth-service`) support Google Workspace SMTP relay. They share
`go-common/mailtransport`, which requires verified STARTTLS before sending mail or
credentials. Templates, recipient selection, inline images, opt-out rules and
throttling stay in their existing services.

## Workspace setup

In Google Admin, Apps → Google Workspace → Gmail → Routing → SMTP relay service:

- Name the rule `CleanApp production outgoing mail`.
- Permit only registered Workspace users as senders.
- Allow only the production server's verified static public egress address. On
  2026-10-10 this was `34.122.15.16`; confirm it before future infrastructure moves.
- Require TLS. IP authorization needs no mailbox password and no domain-wide
  delegation. Do not enable unrestricted relay or broad IP ranges.
- Verify `info@cleanapp.io` has an active Gmail license. It was an active Google
  Workspace for Nonprofits user on 2026-10-10.

The server chooses `info@cleanapp.io` for both the envelope sender and From header.
The relay rule grants the allowed server permission to send as registered domain
users, so preserve the server's existing restricted administrative access.
Relay mail does not create a message in the mailbox's Gmail Sent folder.

Google can take up to 24 hours to apply routing settings. Test authorization from
the production server before enabling application sends.

## Domain authentication

`cleanapp.io` uses Namecheap BasicDNS. Its apex SPF TXT record is
`v=spf1 include:_spf.google.com ~all`, saved and verified on 2026-10-10 while
preserving all existing DNS records. Include any additional active direct senders
in the same SPF record; do not create a second SPF record.

Domain DKIM was enabled on 2026-10-10 using a 2048-bit key with selector
`cleanapp20261010` and the public TXT record at
`cleanapp20261010._domainkey.cleanapp.io`. Google Admin → Gmail → Authenticate
email shows `Authenticating email with DKIM`. An externally received production
test message passed `dkim=pass header.i=@cleanapp.io` and `dmarc=pass`. Preserve
this signing record. Google's default `gappssmtp.com` signature previously passed
DKIM without aligning with `cleanapp.io`.

The existing `_dmarc` policy remains `p=none`. SPF is published in authoritative
and public DNS, but the first post-cutover Gmail messages still reported
`spf=none` from cached pre-publication DNS. Recheck recipient headers after caches
expire; do not infer SPF acceptance solely from DNS publication.

Verify external delivery shows aligned `spf=pass` and `dmarc=pass`; after enabling
domain DKIM, verify `dkim=pass header.d=cleanapp.io`. DNS publication alone does
not establish recipient authentication or inbox delivery.

## Service configuration

Set these variables for **both** active services:

```text
EMAIL_PROVIDER=google_workspace
EMAIL_FROM_NAME=CleanApp
EMAIL_FROM_ADDRESS=info@cleanapp.io
SMTP_HOST=smtp-relay.gmail.com
SMTP_PORT=587
SMTP_TIMEOUT=30s
SMTP_USERNAME=
SMTP_PASSWORD=
```

The checked-in compose files expose these variables. Their provider defaults to
`sendgrid` until the explicit cutover, so merging source alone does not enable
notification delivery. Existing `SENDGRID_FROM_NAME` and `SENDGRID_FROM_EMAIL`
remain fallback sender settings for old configurations. `EMAIL_PROVIDER=sendgrid`
selects the legacy transport for rollback; there is no automatic provider fallback
after a Google failure.

For an SMTP app-password configuration instead of IP relay, set `SMTP_HOST` to
`smtp.gmail.com`, set `SMTP_USERNAME` to the account, and provide `SMTP_PASSWORD`
through the deployment secret mechanism. The transport also accepts
`SMTP_PASSWORD_FILE`; the file must be mounted inside each container. Never put
credentials in git or command arguments. App passwords need account authorization
and have lower Gmail account sending limits than relay.

Keep `email-service-v3` disabled during this migration; enabling it would introduce
a second notification worker. Legacy `email_sender` and the commented backend
mailer are outside the active production path.

## Release and verification

1. Capture current image digests, effective non-secret configuration and compose
   files in a private rollback directory on the production VM.
2. Run Go checks in `go-common`, `email-service` and `auth-service`, including SMTP
   TLS, MIME, failure and provider tests. Scan the release diff for secrets.
3. Build the exact approved commit through
   `platform_blueprint/deploy/prod/vm/source_build_and_deploy.sh` with
   `SOURCE_SERVICES="email-service auth-service"`. Use freshly built immutable
   digests; `build_image.sh -e prod` alone only promotes old image tags.
4. Preserve unrelated effective production configuration. Update both active
   services' provider and SMTP settings, and recreate only these services.
5. Confirm `/health` and `/version`, selected provider, sender and fresh digests.
   Send one controlled internal/external smoke email, then inspect its raw headers
   for sender and authentication results. SMTP DATA success means Google accepted
   the message; it does not prove final recipient delivery.
6. Check new notification delivery rows record `google_workspace`, and custom/case
   results record SMTP Message-ID, while historical SendGrid records retain their
   original provider.
   Check password reset delivery with an authorized test account.

If rollback is needed, restore the saved compose settings and image digests for
these two services only. An expired SendGrid account will still reject mail after
rollback; the retained configuration is for restoring application behavior.

SMTP acceptance failures propagate to the caller and are not recorded as successful
sends. The transport does not retry a send automatically because an ambiguous DATA
response can otherwise create duplicates. Existing service scheduling remains in
control. Workspace relay imposes sending and recipient quotas; monitor rejected
sends before increasing notification volume.

## Production rollout record (2026-10-10)

PR #143 merged as `d205fee705eac9bfd87eec6cccbdfb7bb575bb5c` after all release
checks passed. Both services were built from this commit and deployed through the
canonical source-build/digest workflow with no schema migrations:

- Auth: `sha256:a5188dbcb413f4085d7d05cc81e2dcc5881447f0027db909bf2381e620e157fc`.
- Email: `sha256:1475dd2a372eda28f81d8ee8376e3f7e9518820cef5b3a1ad35482541b4a836f`.

Runtime health, embedded commit, provider, sender and verified TLS passed. Critical
auth settings and all 35 unrelated containers/digest pins were unchanged. Both
production application and password-reset test messages reached the owned Gmail
inbox. Later domain-signed tests passed DKIM and DMARC, but Gmail placed them in
Spam due to previous `cleanapp.io` messages being marked as spam. Do not equate
authentication success with inbox placement or modify mailbox classification to
hide a test result.

At 22:51:19 UTC, natural report notifications had recorded 7,101 report-recipient
delivery links for 1,951 reports and 132 distinct recipients as
`google_workspace`. These are report-recipient rows, not individual SMTP messages.
No fresh provider errors were observed. The first candidate query took 5m44s;
selection, scheduling and candidate SQL were unchanged from the old deployed
commit.

Private rollback files are in
`/home/deployer/incidents/20261010-workspace-mail`. The old auth image has no
registry digest, so retain its local image ID and use `--pull never` for rollback.

References: [Google SMTP relay setup](https://knowledge.workspace.google.com/admin/gmail/advanced/route-outgoing-smtp-relay-messages-through-google),
[application SMTP options](https://knowledge.workspace.google.com/admin/gmail/send-email-from-a-printer-scanner-or-app),
[Gmail sending limits](https://knowledge.workspace.google.com/admin/gmail/gmail-sending-limits-in-google-workspace),
[SPF setup](https://knowledge.workspace.google.com/admin/security/set-up-spf),
[DKIM setup](https://knowledge.workspace.google.com/admin/security/set-up-dkim),
[DMARC setup](https://knowledge.workspace.google.com/admin/security/set-up-dmarc).
