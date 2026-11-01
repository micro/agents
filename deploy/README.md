# Run the agents together

These systemd units run the repository's four binaries on one Linux host.
There is no additional runtime. Support reads this repo and Go Micro; DevOps
inspects the units; Monitor records their process state. Release uses a GitHub
Actions workflow you configure.

## Install

From a checkout, build the binaries with Go 1.26 or newer:

```sh
mkdir -p bin
for agent in support devops release monitor; do
  go build -o "bin/$agent" "./$agent"
done
sudo useradd --system --home-dir /var/lib/micro-agents --shell /usr/sbin/nologin micro-agents
sudo install -d /opt/micro-agents /etc/micro-agents
sudo install -m 755 bin/support bin/devops bin/release bin/monitor /opt/micro-agents/
sudo install -m 644 deploy/systemd/*.service /etc/systemd/system/
sudo install -m 600 /dev/null /etc/micro-agents/common.env
sudoedit /etc/micro-agents/common.env
```

Create the user only if it does not already exist. On upgrades, preserve existing
environment files. Put your model settings in `common.env`:

```ini
MICRO_AI_PROVIDER=openai
MICRO_AI_API_KEY=your-model-key
```

Other providers work too. The common file is required by all units. Optional
`support.env`, `devops.env`, `release.env` and `monitor.env` in the same directory
override it for each agent. Keep credential files root-owned with mode 600;
systemd loads them before switching to the service user.

Support needs no GitHub token for public reads. Add `GITHUB_TOKEN` to
`support.env` for higher rate limits or private repository access. Do not share
Release's write token through `common.env`.

Release still needs an existing `workflow_dispatch` workflow. There is no
deployment workflow in this repository yet. Configure `release.env` using the
[release instructions](../release), including `GITHUB_REPOSITORY`,
`RELEASE_WORKFLOW` and a suitably scoped `GITHUB_TOKEN`. Do not start Release
until that is configured; Monitor will report it as inactive or missing.

```sh
sudo systemctl daemon-reload
sudo systemctl enable --now micro-support micro-devops micro-monitor
# After configuring release.env:
sudo systemctl enable --now micro-release
```

The DevOps unit joins `systemd-journal` to read logs. That account-level permission
can read more than the agent's configured allowlist. Adjust the group for your
distribution. No agent runs as root or has permission to restart these units.

## Use

Use a current Go Micro CLI on the same host, with its default local discovery:

```sh
micro chat support
micro chat devops
micro chat monitor
micro chat release
```

The servers bind to loopback. Keep the registry and host trusted; these examples
do not add RPC authentication or isolate untrusted local users.

| Setting | Default | Override |
| --- | --- | --- |
| Support repositories | micro/go-micro, micro/agents | `SUPPORT_REPOSITORIES`; legacy `GITHUB_REPOSITORY` selects one |
| DevOps units | All four micro-*.service units above | `DEVOPS_SERVICES`, comma-separated exact names |
| Monitor units | Same as DevOps | `MONITOR_SERVICES` |
| Release target | Explicit configuration required | `GITHUB_REPOSITORY`, `RELEASE_WORKFLOW`, `RELEASE_REF` |

To inspect only installed units, set `DEVOPS_SERVICES` in `common.env` to that
subset. Restart the affected units after changing configuration. Running an agent
with `go run` does not install a systemd unit, so these checks will report missing
units until the deployment above is installed.
