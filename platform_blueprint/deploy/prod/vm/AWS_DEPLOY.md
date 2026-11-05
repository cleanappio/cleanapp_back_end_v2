# Portable AWS backend releases

`aws_image_deploy.py` is the AWS path. The existing `source_build_and_deploy.sh`,
`build_image.sh` and `deploy_with_digests.sh` remain historical GCP paths and must
not be used against the AWS host. The image helper works with EC2 or Lightsail
without a registry. The EC2 workflow uses short-lived GitHub OIDC
credentials solely to open and remove its current runner IPv4 `/32` SSH rule;
it does not store a static AWS key or open SSH to the internet.

The first migration must separately restore the captured production images,
data, secrets, mounts, networks, nginx and complete running-role configuration.
This release helper only replaces selected **already running Compose roles**.
It cannot bootstrap a new host, move data, alter DNS, deploy manual containers,
or activate dormant analyzer/social-worker roles.

## GitHub configuration

The manually initiated `deploy-aws.yml` workflow only runs on `main`, checks out
the exact event SHA and preserves the existing CI and gitleaks workflows. Merge
through the normal green-CI policy before running it. It defaults to a dry run;
set `dry_run=false` to build/import/deploy. Supply explicit Compose role names,
for example `cleanapp_report_listener cleanapp_report_analyze_human`. The latter
does not select `cleanapp_report_analyze_pipeline`.

Set these repository variables. The job deliberately has no GitHub environment
so its default OIDC subject can include the exact main branch:

| Variable | Value |
| --- | --- |
| `AWS_DEPLOY_HOST` | Explicit dedicated deployment `user@host`; there is no source-host default. |
| `AWS_DEPLOY_DIR` | Absolute deployment-user-owned directory for release state and managed image pins, separate from the root-readable private baseline. |
| `AWS_COMPOSE_FILES_JSON` | JSON array of absolute target baseline/runtime Compose paths, in merge order. Do not list either managed frontend/backend image override; the helper appends them automatically when present. No source-host path is guessed. |
| `AWS_RUNTIME_ROLE_MANIFEST_JSON` | Non-secret captured evidence in the format below, or the existing `current-runtime.json` shape with `containers[].name` and `containers[].compose["com.docker.compose.service"]`. |
| `AWS_DEPLOY_ROLE_ARN` | Root-verified narrowly scoped OIDC role ARN for this repository's main branch. No static AWS access key is required. |
| `AWS_DEPLOY_SSH_KNOWN_HOSTS` | Pinned public SSH host-key entries verified through an independent trusted channel. |
| `AWS_EC2_SECURITY_GROUP_ID` | Root-verified production EC2 security group ID; its exact ARN must also scope the role's mutation permissions. |

The workflow pins region `us-east-2`; the temporary-ingress helper only manages TCP port 22.
This workflow fixes the image helper's SSH port to 22 as well.

Set repository secret `AWS_DEPLOY_SSH_PRIVATE_KEY`. Use a dedicated deployment identity and host keys
verified through an independent trusted channel. Host-key checking is always
enabled. Do not place private runtime keys, database passwords or signing keys
in build arguments, workflow inputs or role evidence.

```json
{
  "captured_at": "2026-11-05T09:43:15Z",
  "roles": [
    {"service": "cleanapp_report_listener", "container": "cleanapp_report_listener"},
    {"service": "cleanapp_report_analyze_human", "container": "cleanapp_report_analyze_human"}
  ]
}
```

Evidence records the intended running production scope. Every real release
also checks the target's current running state, Compose project/service labels,
and container names before importing or activating anything. A stopped or
missing selected role fails closed.

The target needs Python 3.11+, Docker Engine and current Compose v2 supporting
`config --format json`, `up --wait`, `--pull never` and `pull_policy: never`.
The deployment identity needs noninteractive `sudo docker` and `sudo test -f`
access. The baseline can remain root-readable at
`/opt/cleanapp/private/compose-target.json`; the helper checks its existence
through sudo and runs Compose through sudo, keeping resolved environment values
out of logs. Docker access grants
effective host-root capability; restrict use of that identity accordingly.

## Narrow EC2 ingress and OIDC

The EC2 SSH baseline allows the operator's `31.10.148.2/32`.
Hosted runners cannot connect through that rule. `aws_ci_ingress.py` records
recovery state before mutation, rejects broad SSH ingress, preserves existing
rules and opens only its current public runner IPv4 `/32` using
`AuthorizeSecurityGroupIngress`. Cleanup uses `RevokeSecurityGroupIngress` to
remove only the run's recorded owned `SecurityGroupRuleId`. Existing/foreign
rules are never removed, and parallel runs do not replace one another's rules.

The workflow requests a one-hour OIDC session immediately before opening its
rule. Its `always()` cleanup checks the saved state and refreshes the same
narrow OIDC role before closing, so a long runner build does not depend on the
original session still being valid. Dry runs request no AWS credentials and
make no firewall API calls. A lost or forcibly cancelled runner can prevent
cleanup; retained state identifies the owned rule to remove without modifying
operator/other-run rules. Duplicate or preexisting rules do not establish
ownership and must not be deleted by that run's cleanup. If concurrent runners
share a public IPv4, serialize them so the rule owner does not close shared
access while another run still depends on it.

The role trust must use `sts:AssumeRoleWithWebIdentity`, the verified account's
GitHub OIDC provider, `aud=sts.amazonaws.com`, and the repository's **actual**
exact main-ref subject. For the legacy default format this is
`repo:cleanappio/cleanapp_back_end_v2:ref:refs/heads/main`. Verify the current
repository OIDC format before creating trust: repositories using immutable
owner/repository IDs have a different subject. Do not use a wildcard subject
or the environment-only subject. The workflow's main-branch `if` supplements
the AWS trust policy; it does not replace it.

Grant only the following AWS permissions, with `aws:RequestedRegion` restricted
to `us-east-2` on both statements:

| Actions | Resources |
| --- | --- |
| `ec2:DescribeSecurityGroups`, `ec2:DescribeSecurityGroupRules` | `*` (these read actions have no resource-level scope). |
| `ec2:AuthorizeSecurityGroupIngress`, `ec2:RevokeSecurityGroupIngress` | The exact root-verified production security group ARN; do not substitute an account-wide group wildcard. |

Do not grant instance lifecycle, IAM, or unrelated data-plane permissions.
EC2 IAM does **not** expose condition keys that constrain the requested
port/CIDR parameters of `AuthorizeSecurityGroupIngress`. The exact security
group restriction is enforced by IAM; TCP 22, public `/32`, baseline
preservation and owned-rule cleanup are enforced by the reviewed helper.
Anyone able to alter trusted main-branch workflow code and assume this role
can change ingress on that one security group within the granted actions.
Preserve branch/CI protections and verify the helper before merging.

The AWS API contract is documented in
[AuthorizeSecurityGroupIngress](https://docs.aws.amazon.com/AWSEC2/latest/APIReference/API_AuthorizeSecurityGroupIngress.html)
and
[RevokeSecurityGroupIngress](https://docs.aws.amazon.com/AWSEC2/latest/APIReference/API_RevokeSecurityGroupIngress.html).
AWS explains the
[port/CIDR condition-key limitation](https://aws.amazon.com/blogs/security/can-i-do-that-with-policy-understanding-the-aws-service-authorization-reference/).
GitHub documents environment, branch and immutable subject formats in its
[OIDC reference](https://docs.github.com/en/actions/reference/security/oidc).

## Release behavior

The helper archives the exact selected commit and builds `linux/amd64` images
on the runner. It never invokes nested legacy build scripts. Service contexts
retain their common Go/Rust modules; root backend/pipelines builds retain their
root context. Untracked files cannot enter an image, and credential filenames
and environment files are excluded from contexts. Existing tracked version
files stay unchanged. Generated build metadata uses the tracked version plus
`-aws.<commit12>`, with full source SHA, build time and OCI source/version labels.

Selected Go `cmd/migrate` tools are built into separate minimal images from the
same commit. Migrations run once per selected source using the host's Compose
environment, mounts and network; secret values never leave the host. Set
`skip_migrations=true` only for an explicitly reviewed no-schema-change release.
Services with startup migrations retain that existing application behavior.

Docker image archives are compressed and streamed over pinned SSH, with an
archive SHA256 in `image-manifest.json`. Before migrations or service changes,
the target checks the transfer checksum, imported Docker image IDs, architecture
and source/version labels. Imported images are pinned by Docker configuration
image ID, which is distinct from a registry manifest digest.

The persistent `docker-compose.aws-backend-images.yml` uses JSON (valid YAML)
and contains only backend
image-ID pins and `pull_policy: never`. Every partial release retains all prior
unselected backend pins. The frontend helper owns its separate
`docker-compose.aws-frontend-images.yml`; neither helper overwrites the other.
The host's reboot, watchdog and operational Compose entrypoint must include
both persistent image overrides after the verified baseline/runtime overrides.
The backend helper appends the existing frontend override first and its own
backend override last. Caller lists containing either managed image filename
are rejected, so caller merge order cannot override these pins.

Activation always names selected roles and uses `up -d --no-deps --no-build
--pull never --wait`. It never calls broad `up`, `pull`, `down`, pruning or
`--remove-orphans`. The target verifies the selected containers' actual image
IDs after Compose reports them healthy/running. Services without a healthcheck
receive running-state checks; validate their public behavior separately.

Each target `.aws-releases/<release>/` keeps the manifest, prior image-ID pins
in `rollback.yml` and `rollback.json`, and successful `result.json`. The extra
transport archive is deleted after the activation attempt; no image pruning is
performed. Runner evidence contains only image identity and non-secret scope,
is retained for seven days, and includes the target release path on failure.

If a migration fails, service replacement and persistent image-pin changes do
not run. If activation/health validation fails, the workflow fails and keeps
the new pins plus explicit rollback pins for recovery. Inspect the host
privately; the helper suppresses effective Compose environment and command
error output. For application rollback, merge the saved `rollback.yml` after
the normal Compose files and both persistent image overrides, then run scoped
`up -d --no-deps --no-build --pull never --wait <same selected roles>`. Persist
the reviewed rollback pins in the backend image override before a later reboot.
Database schema changes are not automatically reversed; verify compatibility
and use the independent verified data backup if database recovery is required.

## Local validation

```bash
python3 -W error -m unittest discover \
  -s platform_blueprint/deploy/prod/vm/tests -p 'test_*.py'
```

An explicit local `--dry-run` accepts the same host, key-file, known-hosts,
runtime-manifest and Compose-path configuration but makes no Docker/SSH calls.
The checked-in contract tests use isolated fake commands and no cloud access.

Docker documents scoped activation and pull controls in
[Compose up](https://docs.docker.com/reference/cli/docker/compose/up/),
one-off migration behavior in
[Compose run](https://docs.docker.com/reference/cli/docker/compose/run/), and
the [service specification](https://docs.docker.com/reference/compose-file/services/).

## Backup and isolation coordination

Activation takes an exclusive `fcntl.flock` on `/opt/cleanapp/deploy/.deployment-and-backup.lock`, the same stable inode used by weekly backup and cold restore. The parent must be deployer-owned and not group/world writable. The lock must remain a non-symlink, single-link regular file owned by the deployer with mode `0600`; the helper does not replace or change an existing lock. It holds the lock across runtime inspection, migrations, image pins, restart and verification, releasing it on exceptions. Runner builds and SSH archive uploads are outside this lock. Any operator rollback using the recorded manifest must acquire this same existing lock and review the database migration boundary first.

The helper requires `/etc/cleanapp-runtime/ISOLATED` to be absent (including dangling symlinks) and `/etc/cleanapp-runtime/ACTIVE` to be a regular non-symlink file. Sudo must permit `test -f`, `test ! -e` and `test ! -L` checks in addition to Docker. A manual GitHub trigger cannot bypass the migration/restore one-writer fence.
