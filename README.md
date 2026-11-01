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

## Development

Requires Go 1.26 or newer.

```sh
go test -race ./...
go build ./...
```

Licensed under AGPL-3.0; see [LICENSE](LICENSE).
