# DevOps

Investigate service failures using health observations and logs. Run through
`micro run` and ask `micro chat` to investigate a named service.

By default, DevOps reads the exact logs written by `micro run` for this project's
agents and queries Monitor for their RPC health. It does not run shell commands
or control processes. Running with `go run ./devops` alone will not create those
log files; use the repo's `micro run` entry point.

```sh
micro call devops Process.List '{}'
micro call devops Process.Logs '{"service":"support","lines":100}'
micro call devops Monitor.Status '{}'
```

Logs are restricted to the configured service set in the current Go project,
limited to 64 KiB and 200 lines, and marked when truncated. Log content is sent
to the configured model when used in chat.

## Systemd

Set `DEVOPS_SERVICES` to exact unit names to use systemd inspection instead:

```sh
export DEVOPS_SERVICES=micro@support.service,micro@release.service
```

This mode provides `Systemd.List`, `Systemd.Status` and `Systemd.Logs`. See
[deploy](../deploy) for running all agents under Go Micro's systemd template.
The account needs permission to read those journals. Systemd log queries cover
at most 24 hours and 200 entries. The agent never invokes sudo.

Both modes are read-only. Findings include evidence and recommended actions;
the agent cannot restart, deploy, change files or claim to have repaired anything.
