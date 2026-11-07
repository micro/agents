# Release

Release and deploy through an existing GitHub Actions workflow. Ask what it does,
start a release after approval, and inspect its progress or failed steps.

## Run

With no configuration, Release inspects workflows in `micro/studio` and cannot
dispatch one. To enable releases, the target repository needs a workflow with
`workflow_dispatch` on its default branch.
Your workflow owns builds, tags, publishing, deployments and environment policy.
This agent does not create a pipeline or deploy directly to a hosting provider.

From the repository root:

```sh
export GITHUB_REPOSITORY=owner/repo
export GITHUB_TOKEN=your-github-token
export RELEASE_WORKFLOW=release.yml
export MICRO_AI_PROVIDER=openai
export MICRO_AI_API_KEY=your-model-key
go run ./release
```

`RELEASE_WORKFLOW` is the filename in `.github/workflows/`. `RELEASE_REF` defaults
to `main`; set it to the trusted branch or tag whose workflow should run. Workflow
inputs such as version and environment are supplied in the conversation. The
token needs Contents read and Actions read/write access to this repository.
Without a token, public workflow inspection still works; dispatch is disabled.

In another terminal with a current Go Micro CLI:

```sh
go install go-micro.dev/v6/cmd/micro@master
micro chat release
```

Ask **“What does our release workflow do?”**, then **“Release version 1.2.3 to
staging.”** The agent reads the workflow, asks for any missing inputs and presents
the dispatch for approval. After approval it returns a run link. Ask **“How is
that release going?”** to check it. It does not monitor continuously.

The agent binds to loopback by default. Run the CLI on the same machine using the
same registry. The model key belongs to the agent. Other model providers and
`MICRO_AI_MODEL` work as with the other agents.

## Service

`services/github.Actions` is a separate handler from support's read-only GitHub
handler. It is restricted to one repository, workflow and dispatch ref.

```sh
micro call release Actions.Workflow '{}'
micro call release Actions.Runs '{}'
micro call release Actions.Run '{"id":123456}'
micro call release Actions.Jobs '{"id":123456}'
```

`Runs` and `Jobs` accept a `page`; `more` indicates another page. Responses are
bounded. Dispatch returns `accepted`, a run `id` when available, and a `url`.
Accepted is not a successful deployment. A timeout can mean the workflow started;
inspect runs before trying again. The agent disables automatic RPC retries.

Chat approval protects agent tool calls. Direct `Actions.Dispatch` RPC calls are
trusted operations and bypass chat approval; this is not an authenticated service.
Keep it local, restrict access to its registry and token, and use GitHub environment
protection rules for production deployments. Workflow definitions and run metadata
are sent to the configured model; do not put secrets in workflow inputs.

## Verify

```sh
go test -race ./...
go build ./...
go vet ./...
```

Tests use local GitHub fixtures. Before using a real deployment workflow, point
the agent at a disposable `workflow_dispatch` workflow. Check that declining
approval starts no run, approving starts one run, and its ID matches GitHub.
Verify running, failed and successful states. Automated tests do not establish
live-model accuracy, token permissions or the correctness of your deployment.
