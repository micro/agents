# Micro Studio

A workspace for agents.

Self-hosted and built with [Go Micro](https://github.com/micro/go-micro).

Discover running services, inspect their endpoints, chat with agents and see
health and alerts in one place. Studio uses your existing Go Micro registry;
your applications remain ordinary Go programs.

## Run

Requires Go 1.26 or newer, with Go's bin directory on your PATH.

```sh
go install go-micro.dev/v6/cmd/micro@master
git clone https://github.com/micro/studio
cd studio
```

Set your model configuration, then start Studio and its built-in agents:

```sh
export MICRO_AI_PROVIDER=openai
export MICRO_AI_API_KEY=your-model-key
micro run
```

Open **http://localhost:8080**. Select an agent to start a conversation; responses
include tool calls and results. Services shows discovered endpoints and schemas.
Alerts shows incidents and notification delivery. The CLI remains available with
`micro chat` in another terminal using the same model and registry settings.

Studio is currently a local, single-operator workspace. It binds to loopback and
has no user accounts. For a server, use an SSH tunnel; see [deployment](deploy).
Chat is request/response, with tool activity shown when the agent finishes.
Conversations stay separate per agent and browser tab; reloading starts a new
conversation. Release actions that need interactive approval remain available
through the CLI; Studio does not grant approval or expose arbitrary RPC writes.

## Built-in agents

- [Support](support): investigate GitHub issues and draft responses.
- [DevOps](devops): inspect service health and logs.
- [Release](release): inspect releases and run configured workflows through the CLI.
- [Monitor](monitor): check service responsiveness and record changes.
- [Alert](alert): record incidents and deliver failure and recovery notifications.

Support covers Go Micro and this repository by default. Monitor and Alert run
without model calls. Configure these agents for your environment, or connect
Studio to your existing registry. To run only the web workspace: `go run ./cmd/studio`.

## Extend

Write services and agents using Go Micro. Run them with the same registry as
Studio and they appear automatically. Studio identifies agents by their
`Agent.Chat` endpoint. Add local programs to `micro.json` to start them with
`micro run`. Reusable integrations live in `services/`.

No Studio-specific SDK or application format is required. Service registration
indicates discovery, not health; health observations come from Monitor's configured
service set. The current workspace does not manage deployments, credentials or
agent definitions.

## MCP

Expose the same running services to another client:

```sh
micro gateway --mcp-address 127.0.0.1:3000
```

Connect to **http://127.0.0.1:3000/mcp** using Streamable HTTP. The gateway discovers
tools automatically; it must use the same registry. The default is local mDNS.

The gateway includes write endpoints by default. Restrict tools with
`--mcp-tool <tool-name>` before exposing deployment actions: MCP calls do not
inherit chat approval. Remote gateways require HTTPS and `--auth` with
`MICRO_AUTH_TOKEN` and a matching bearer token in the client.

## Development

```sh
go test -race ./...
go build ./...
```

With `micro` and `nats-server` installed, run `python3 scripts/smoke.py` to test
startup, Studio web chat, CLI delegation, service failure, alert delivery and
recovery. The test uses local fixtures and temporary state, without real model
credentials. Set `MICRO_BIN` to select a CLI binary.

Licensed under AGPL-3.0; see [LICENSE](LICENSE).
