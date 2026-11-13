# CLI release to npm

The manual release workflow is `.github/workflows/cleanapp_cli_publish.yml`. It runs only from this repository's `main` branch, checks the exact reviewed source SHA and expected package version, tests/builds the CLI, and retains the tested tarball with SHA-256 and npm integrity evidence. `publish=false` is the default: it prepares the artifact without publication. `publish=true` publishes that same packed file through GitHub OIDC. No static npm token is passed, changed or deleted.

## One-time owner configuration

The package owner configures a GitHub Actions trusted publisher for `@cleanapp/cli` in npm package Settings → Trusted publishing:

| Field | Exact value |
| --- | --- |
| Organization or user | `cleanappio` |
| Repository | `cleanapp_back_end_v2` |
| Workflow filename | `cleanapp_cli_publish.yml` |
| Environment name | `npm-publish` |
| Allowed action | Enable **Allow npm publish** |

Current new trusted-publisher configurations allow staging by default; direct `npm publish` must be selected explicitly. Root maintains the GitHub `npm-publish` environment with exactly the `main` deployment branch allowed and no recurring reviewer gate. Its independent branch restriction prevents a branch edit from removing the workflow's own main guard and publishing through that trusted identity.

Use the [official publisher setup and allowed-actions instructions](https://docs.npmjs.com/trusted-publishers/#for-github-actions). npm requires Node ≥22.14.0 and npm ≥11.5.1; this workflow uses Node24 and pins npm11.21.0 in a temporary local prefix. This is separate from the CLI runtime's Node≥20 requirement. A newly created trusted-publisher configuration must complete its first successful publication within two days. See [npm's configuration lifetime](https://docs.npmjs.com/trusted-publishers/#trusted-publisher-configuration-expiry).

`package.json` has the canonical repository URL and `cli/cleanapp` directory required to bind the public package to its GitHub source. OIDC publication generates provenance automatically for this public repository/package. `npm whoami` is not a trusted-publisher permission check; it is useful only for a separate legacy credential diagnosis. The [official npm guidance](https://docs.npmjs.com/trusted-publishers/) distinguishes these operations.

## Reviewed manual release

1. Merge the reviewed release workflow and package metadata into `main`.
2. Record that exact main SHA and verify the CLI version in `package.json`, both lockfile locations and `src/version.ts`.
3. Dispatch the workflow on `main` with that full `reviewed_sha` and `expected_version` (currently `0.2.0`). Keep `publish=false` for a preparation-only run.
4. Inspect the retained `cleanapp-cli-VERSION-SHA` artifact and `release-evidence.json`, including its source, package name/version and tarball SHA-256.
5. For the intended release, dispatch with the same reviewed main SHA/version and `publish=true` after the owner configuration matches the table. The workflow publishes its tested packed tarball, with no `NPM_TOKEN` or `NODE_AUTH_TOKEN` fallback.

If main advances, select/review the new main SHA or the guard stops the run; it never checks out an older input while claiming a newer workflow identity. Publication is serialized, and a version already present on npm remains immutable under npm's existing rules. No branch/tag push automatically publishes a package.

## Interim GitHub tarball

While npm 0.2.0 is unavailable, use the live MCP/public HTTP entry at https://www.cleanapp.io/agents. The tested 0.2.0 fallback tarball is prepared for a GitHub release named `cleanapp-cli-v0.2.0`, asset `cleanapp-cli-0.2.0.tgz`, on this backend repository. Root publishes that reviewed release/asset separately. Its digest and source are recorded alongside the prepared tarball; do not label npm `latest` 0.1.1 as having 0.2.0's no-key behavior.

After the asset is live, the CLI README provides a direct local-prefix npm installation URL. No global/root install or new authentication flow is needed for the fallback.
