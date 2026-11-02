# Agents

Agents built with [Go Micro](https://github.com/micro/go-micro).

- [Alert](alert): record incidents and deliver failure and recovery notifications.
- [DevOps](devops): investigate service failures using health observations and logs.
- [Monitor](monitor): check that the agents respond and record health changes.
- [Release](release): release and deploy through a configured GitHub Actions workflow.
- [Support](support): investigate GitHub issues and draft responses with supporting evidence.

Each agent is a Go program. Service integrations live in `services/` and can also
be called directly over RPC. Run agents in your own environment with your own
model configuration.

By default, Support covers this repository and Go Micro. Monitor, Alert and
DevOps look after the agents running here. Override their targets for your own
environment; see [deploy](deploy) for server installation.

## Install

Requires Go 1.26 or newer, with Go's bin directory on your PATH.

```sh
go install go-micro.dev/v6/cmd/micro@master
git clone https://github.com/micro/agents
cd agents
```

Set your model credentials. For example, with OpenAI:

```sh
export MICRO_AI_PROVIDER=openai
export MICRO_AI_API_KEY=your-model-key
micro run
```

In another terminal, with the same model settings:

```sh
micro chat
```

`micro run` uses `micro.json` to build and start all five agents; Ctrl+C stops
them. Release starts in inspection-only mode until you [configure a workflow](release#run).
Monitor and Alert work in the background without model calls. Each agent has its
own persistent store database. Run chat on the same host with the same registry.

## MCP

Expose running agents and services to an MCP client through Go Micro's gateway.
With `micro run` running, start the gateway in another terminal on the same host:

```sh
micro gateway --mcp-address 127.0.0.1:3000
```

Add a **Streamable HTTP** MCP server in your client with this URL:

```text
http://127.0.0.1:3000/mcp
```

The gateway automatically exposes discovered service endpoints as tools.
The client lists and calls them without configuring each tool individually.
The gateway and agents must use the same registry; the default is local mDNS.
An MCP client only needs the gateway connection, not registry access.

For remote access, put the gateway behind HTTPS, enable `--auth` with
`MICRO_AUTH_TOKEN`, and configure the client to send that bearer token. Use the
public HTTPS URL ending in `/mcp`, rather than the loopback URL above.

To expose only a subset, optionally repeat `--mcp-tool <tool-name>`.
The default includes write endpoints such as Release's dispatch operation.
MCP calls do not inherit chat approval; restrict exposed tools before giving
clients access to deployment actions.

## Development

```sh
go test -race ./...
go build ./...
```

To test the full CLI journey with local model/webhook fixtures and a NATS
registry, install `nats-server`, then run `python3 scripts/smoke.py`. Set
`MICRO_BIN` to select a CLI binary. The test starts all agents, delegates from one
chat, reads failure logs through DevOps, stops and restarts Support, verifies
failure/recovery delivery, and checks
Alert persistence. It uses temporary state and no real provider credentials.

Licensed under AGPL-3.0; see [LICENSE](LICENSE).
