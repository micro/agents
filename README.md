# Agents

Agents built with [Go Micro](https://github.com/micro/go-micro).

- [Support](support): investigate GitHub issues and draft responses with supporting evidence.

Each agent is a Go program. Service integrations live in `services/` and can also
be called directly over RPC. Run agents in your own environment with your own
model configuration.

## Development

Requires Go 1.26 or newer.

```sh
go test -race ./...
go build ./...
```

Licensed under AGPL-3.0; see [LICENSE](LICENSE).
