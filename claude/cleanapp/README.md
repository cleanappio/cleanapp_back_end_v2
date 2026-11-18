# CleanApp for Claude

Ask Claude to report a problem you observed — a broken website, an app error, an accessibility barrier, a pothole, illegal dumping — and it prepares the report, shows you the draft, submits it to CleanApp once you approve, and tracks the decision.

The plugin has two parts:

- `.mcp.json` connects the public CleanApp MCP server, `https://live.cleanapp.io/openai/mcp` (Streamable HTTP, no authentication). This is the same server the ChatGPT and Grok connectors use.
- `skills/report` tells Claude how to use it: prepare, show the draft and its handling permissions, submit the approved draft, report the receipt and outcome.

No account or API key is needed. Operating rules: https://www.cleanapp.io/agents.txt. Privacy: https://live.cleanapp.io/openai/privacy.

## Install

### Claude Code

```bash
claude plugin marketplace add cleanappio/cleanapp_back_end_v2
```

```bash
claude plugin install cleanapp@cleanapp
```

Then ask: "Report to CleanApp that the checkout button on example.com does nothing", or run `/cleanapp:report`.

To add only the connector, without the skill:

```bash
claude mcp add --transport http cleanapp https://live.cleanapp.io/openai/mcp
```

### Claude on the web, desktop and mobile

Until CleanApp is listed in Claude's directory, add it as a custom connector:

1. Open **Customize > Connectors**.
2. Choose **+ Add**, then **Add custom connector**.
3. Name it `CleanApp` and enter `https://live.cleanapp.io/openai/mcp`.
4. For authentication choose **No sign in**, then **Add**.

Team and Enterprise owners add it once under **Organization settings > Connectors** (**Add > Custom > Web**); members then connect it from **Customize > Connectors**.

## What a receipt means

A receipt confirms intake. Analysis then returns a decision, and publication and notice delivery are recorded separately. None of these establishes that the problem was fixed.

## Maintaining

Validate before each release, and increment `version` in `.claude-plugin/plugin.json` so installed copies update:

```bash
claude plugin validate --strict ./claude/cleanapp
```

```bash
claude plugin validate .
```
