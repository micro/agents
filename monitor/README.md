# Monitor

Check whether the agents respond to RPC. Run with the rest of the repo using
`micro run`, then ask `micro chat` what is healthy or what changed.

Monitor checks every registered instance through `Health.Check` every ten seconds.
The default targets are `support`, `devops`, `release`, `monitor` and `alert`.
Set `MONITOR_SERVICES` to a comma-separated list of registered service names to
replace them. Processes started locally, through systemd or in containers use the
same health contract; they must share discovery and have reachable RPC addresses.

- `healthy`: all registered instances answered `Health.Check` with `ok`.
- `unhealthy`: at least one instance failed the check.
- `missing`: no instance is registered.
- `unknown`: the registry could not be queried or no observation exists yet.

These checks do not call models or test external dependencies. They establish RPC
responsiveness, not whether an agent gives a correct answer. Observations include
timestamps; Alert detects stale or unavailable monitoring data separately.

State and the latest 100 changes persist in the configured store. Run one Monitor
per deployment. Alert owns incident detection and notification delivery; Monitor
does not restart anything.

## Service

```sh
micro call monitor Monitor.Status '{}'
micro call monitor Monitor.History '{}'
```

The service lives in [services/monitor](../services/monitor). Its RPC checker uses
ordinary Go Micro registry/client/store interfaces. The existing systemd checker
is also available programmatically, but is not the default agent mode.
