# Agents

Agents built with [Go Micro](https://github.com/micro/go-micro).

- [DevOps](devops): investigate local systemd service failures using status and journal logs.
- [Monitor](monitor): watch the agents' services and report failures and recoveries.
- [Release](release): release and deploy through a configured GitHub Actions workflow.
- [Support](support): investigate GitHub issues and draft responses with supporting evidence.

Each agent is a Go program. Service integrations live in `services/` and can also
be called directly over RPC. Run agents in your own environment with your own
model configuration.

By default, Support covers this repository and Go Micro. DevOps and Monitor
inspect the units in [deploy](deploy). Follow that guide to run the agents together
on one host, or override their targets for your own environment.

## Install

Requires Go 1.26 or newer, with Go's bin directory on your PATH.

```sh
go install go-micro.dev/v6/cmd/micro@master
git clone https://github.com/micro/agents
cd agents
```

Set your model credentials and configure [Release's workflow](release#run)
before starting the full set. For example, with OpenAI:

```sh
export MICRO_AI_PROVIDER=openai
export MICRO_AI_API_KEY=your-model-key
micro run
```

In another terminal, with the same model settings:

```sh
micro chat
```

`micro run` builds and starts the agents as separate local processes; Ctrl+C
stops them. DevOps and Monitor currently inspect systemd units, which
`micro run` does not create. For a Linux server installation, see [deploy](deploy).

## MCP

Expose running agents and services to an MCP client through Go Micro's gateway.
With `micro run` running, start the gateway in another terminal on the same host:

```sh
micro gateway --mcp-address 127.0.0.1:3000 \
  --mcp-tool support.Agent.Chat \
  --mcp-tool monitor.Monitor.Status \
  --mcp-tool monitor.Monitor.History
```

Add a **Streamable HTTP** MCP server in your client with this URL:

```text
http://127.0.0.1:3000/mcp
```

The client discovers the selected tools and calls them through the gateway.
`support.Agent.Chat` accepts a `message`; the monitor tools accept `{}`.
The gateway and agents must use the same registry; the default is local mDNS.
An MCP client only needs the gateway connection, not registry access.

For remote access, put the gateway behind HTTPS, enable `--auth` with
`MICRO_AUTH_TOKEN`, and configure the client to send that bearer token. Use the
public HTTPS URL ending in `/mcp`, rather than the loopback URL above.

Keep the explicit tool list: omitting it exposes all discovered tools. This example
provides agent replies and service reads; it does not bridge interactive approvals.
Release's write endpoints should remain private until that approval path is supported.

## Development

```sh
go test -race ./...
go build ./...
```

Licensed under AGPL-3.0; see [LICENSE](LICENSE).
