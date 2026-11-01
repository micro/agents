# Monitor

Watch the agents' systemd services. Checks run immediately and once a minute
without model calls. State changes go to the journal and can be queried in chat.

```sh
export MICRO_AI_PROVIDER=openai
export MICRO_AI_API_KEY=your-model-key
go run ./monitor
```

In another terminal on the same machine:

```sh
micro chat monitor
```

Ask **“What is down?”** or **“What changed recently?”**

The default set is `micro-support.service`, `micro-devops.service`,
`micro-release.service` and `micro-monitor.service`, matching [deploy](../deploy).
`DEVOPS_SERVICES` overrides the shared set; `MONITOR_SERVICES` overrides it for
monitoring only. Uninstalled units are reported as missing.

These are process checks. An active unit does not prove that an agent can answer,
its model is available, or an application is healthy. Permission and read errors
are reported as unknown. History holds the last 100 changes in memory and resets
on restart. The journal retains logged changes according to the host's policy.
There are no external alerts or automatic repairs. Use DevOps to investigate logs.

## Service

The read-only service is usable without a model:

```sh
micro call monitor Monitor.Status '{}'
micro call monitor Monitor.History '{}'
```

The agent binds to loopback and uses local discovery. It has no RPC authentication.
The polling service lives in [services/monitor](../services/monitor); its `Run`
method takes a context for shutdown. Only explaining observations requires a model.
