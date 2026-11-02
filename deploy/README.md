# Deployment

The same `micro.json` describes local startup and server deployment. Locally:

```sh
micro run
```

All five agents are separate processes. Monitor checks their RPC endpoints and
Alert records failures and recoveries. No systemd installation is needed locally.
Use `micro chat` as the single entry point.

## Linux server

Use Go Micro's existing systemd deployment model. On a Linux server with the CLI:

```sh
sudo micro init --server
```

This creates the `micro` account, `/opt/micro/{bin,config,data}` and the
`micro@.service` unit. Before deployment, create a mode-600 environment file for
each agent in `/opt/micro/config/`: `support.env`, `devops.env`, `release.env`,
`monitor.env` and `alert.env`. The systemd manager reads these files; keep them
root-owned. Each file needs your model configuration and a writable home:

```ini
HOME=/opt/micro/data
MICRO_AI_PROVIDER=openai
MICRO_AI_API_KEY=your-model-key
```

In `devops.env`, select systemd inspection:

```ini
DEVOPS_SERVICES=micro@support.service,micro@devops.service,micro@release.service,micro@monitor.service,micro@alert.service
```

Grant the `micro` account journal access if needed (`systemd-journal` on many
Linux distributions). This permission can read the host journal beyond the
agent's allowed units. None of the agents needs sudo or service restart rights.

Release starts in inspection-only mode until its workflow is configured. Put its
write token only in `release.env`; see [Release](../release). Optionally configure
`ALERT_WEBHOOK` in `alert.env`; otherwise notifications are written to the journal.
All agents must use the same registry. The default is mDNS; on networks without
multicast, set a shared registry such as NATS in each environment file and in the CLI.

From this checkout, inspect the plan and deploy:

```sh
micro deploy --dry-run user@host
micro deploy user@host
```

The SSH account needs permission to write the target binaries and manage the
systemd units. The current CLI builds for Linux/amd64. It does not provision a
host or configure credentials, and it does not verify an application's behaviour.
The services bind to loopback; run chat and the MCP gateway on that same host.

## Overrides

- `SUPPORT_REPOSITORIES`: comma-separated repositories; defaults to Go Micro and this repo.
- `GITHUB_REPOSITORY`: Release's repository, and the legacy single-repository Support override. Use per-agent environment files to keep these separate.
- `DEVOPS_SERVICES`: explicit systemd units; otherwise DevOps reads this project's `micro run` logs.
- `MONITOR_SERVICES`: registered service names, defaulting to the five agents.
- `ALERT_WEBHOOK`: optional HTTP(S) notification destination, configured by the operator.

Monitor and Alert persist state through Go Micro's configured store. Keep
`/opt/micro/data` across upgrades. Run one Monitor and one Alert per deployment;
their state is not coordinated across replicas. Use separate state directories
and registry scopes for separate deployments.
