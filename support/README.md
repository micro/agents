# Support

Investigate a GitHub issue and draft a response for a maintainer to review.
The agent reads the discussion, repository documentation and related issues.
It cannot post comments, change labels, close issues or execute code.

## Run

By default, support covers both `micro/go-micro` and `micro/studio`.
From the repository root, configure a supported model:

```sh
export MICRO_AI_PROVIDER=openai
export MICRO_AI_API_KEY=your-model-key
go run ./support
```

Set `SUPPORT_REPOSITORIES=owner/one,owner/two` to replace the default set.
The existing `GITHUB_REPOSITORY=owner/repo` setting still selects one repository
when `SUPPORT_REPOSITORIES` is unset. This agent reads issues when asked; it does
not poll GitHub or post notifications in the background.

Provider-specific keys such as `OPENAI_API_KEY` also work. Set `MICRO_AI_MODEL`
when selecting a particular model. Public GitHub repositories work without a
GitHub token, subject to GitHub's unauthenticated rate limits. For private
repositories or higher limits, set `GITHUB_TOKEN` to a token limited to the
selected repository with read access to contents and issues. GitHub data you
retrieve will be sent to your configured model provider.

Install the matching CLI in another terminal:

```sh
go install go-micro.dev/v6/cmd/micro@v6.0.0-20261028182459-ddf8c9c37944
micro chat support
```

Start with `What do you see?` for recently updated open issues, or ask:

```text
Investigate micro/go-micro#5042. Summarise what is still unresolved and draft a response.
```

The result contains an investigation summary, source links and a draft response.
Review it yourself before publishing. No model credential is needed in the chat
client; it belongs in the running service.

The GitHub operations can also be used without asking the model:

```sh
micro call support GitHub.Repositories '{}'
micro call support GitHub.Issues '{"page":1}'
micro call support GitHub.Issue '{"repository":"micro/go-micro","number":5042}'
micro call support GitHub.Comments '{"number":5042,"page":1}'
micro call support GitHub.Search '{"query":"startup error"}'
micro call support GitHub.File '{"path":"README.md"}'
```

Each read accepts `repository`; omitted values use the first configured repository.
`GitHub.File` also lists directories. Comments and search results include `more`
when another page is available. Files come from the default branch; check their
applicability to the reporter's release. Responses larger than 256 KiB return an
error rather than silently truncating evidence.

## Extend

Edit [instructions.md](instructions.md) for your support policy. The GitHub
integration is an ordinary [Go Micro handler](../services/github/github.go).
Register additional read-only service operations in `main.go` when the agent
needs other evidence. Only this service's endpoints are exposed to the agent
by default.

The service uses Go Micro's default local discovery. It binds to loopback by default. Run the CLI on the same machine; this starter does not add RPC authentication or tenant isolation. Do not
expose it publicly with a token that can read private repositories.

## Version and verification

The module and CLI are pinned to Go Micro commit `ddf8c9c37944`, which includes
service-attached agents. This is an unreleased revision, not v6.15.1. The upstream
release/security follow-up is tracked in go-micro issue #5042.

Automated tests cover GitHub reads, repository scope, pagination, response limits,
errors and redirect handling. These tests do not establish live-model answer
quality. Evaluate drafts against actual issues before relying on them.
