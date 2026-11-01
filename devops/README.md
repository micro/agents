# DevOps

Investigate local systemd service failures using current state and journal logs.
The agent reports evidence and a recommended action. It cannot restart services,
deploy, change files or run arbitrary commands.

## Run

Run on the Linux machine you want to inspect, with `systemctl` and `journalctl`
installed. From the repository root:

```sh
export MICRO_AI_PROVIDER=openai
export MICRO_AI_API_KEY=your-model-key
go run ./devops
```

By default it inspects `micro-support.service`, `micro-devops.service`,
`micro-release.service` and `micro-monitor.service`, matching [the deployment
units](../deploy). Set `DEVOPS_SERVICES=api.service,worker.service` to replace
this set. Use exact service names, including `.service`. Only configured units can be
inspected (up to 16). Provider-specific keys and `MICRO_AI_MODEL` also work.
Run as an unprivileged account with permission to read the relevant journal.
The agent does not invoke sudo or change permissions. Journal access groups can
expose logs for the whole host to that account, even though these tools restrict
queries to configured units.

In another terminal, with a current Go Micro CLI:

```sh
go install go-micro.dev/v6/cmd/micro@master
micro chat devops
```

Ask **“What is running?”** or **“Why is micro-support.service failing?”** The model key
belongs to the running agent; the chat client does not need it.

The agent runs on the host being inspected. The CLI must share its registry;
this example uses default local discovery. Like support, this starter does not
add RPC authentication or tenant isolation. It binds to loopback by default; use the CLI on the same machine.
Retrieved status and logs are sent to your configured model provider; use units
whose logs are appropriate to share.

## Tools

These are ordinary Go Micro service endpoints, usable without a model:

```sh
micro call devops Systemd.List '{}'
micro call devops Systemd.Status '{"service":"micro-support.service"}'
micro call devops Systemd.Logs '{"service":"micro-support.service","minutes":60,"lines":100}'
```

Logs default to the last hour and 100 entries, with a maximum of 24 hours and
200 entries. Command output is capped at 64 KiB and marked when truncated.
Commands time out after 10 seconds. Permission errors and journal warnings are
reported as errors rather than presented as an empty journal.

An active systemd unit is not proof of application health. This version has no
metrics, deployment history, network probes or access to other hosts.

## Verify

```sh
go test -race ./...
go build ./...
```

Tests cover allowed units, command arguments, output limits, permission errors,
cancellation and real loopback RPC with fixture status data. They do not establish
live-model diagnostic accuracy or validate your host's journal permissions.

For a manual check, configure a disposable service that exits with an error and
writes a known failure message. Ask the agent to investigate it. Check that it
reports the failed state and cites the actual timestamped message, distinguishes
hypotheses from facts, and does not claim to have repaired it.

## Extend

Edit [instructions.md](instructions.md) for operational policy. The integration
lives in [services/systemd](../services/systemd). Add further service integrations
when the agent needs metrics or deployment evidence; keep operational changes
separate from these read-only tools.
