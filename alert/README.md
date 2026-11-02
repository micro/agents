# Alert

Record failures and recoveries from Monitor and deliver notifications. Alert runs
with the other agents through `micro run`. Ask `micro chat` what needs attention
or whether a notification was delivered.

Two distinct unhealthy observations open one incident. Repeated observations do
not send repeated notifications. A healthy observation queues a recovery for the
same incident. Missing or stale monitoring data opens a separate monitoring incident;
it does not resolve existing service incidents.

Notifications go to the process log by default. To deliver to an HTTP webhook,
configure `ALERT_WEBHOOK` before starting the agents:

```sh
export ALERT_WEBHOOK=https://your-server.example/alerts
micro run
```

The receiver gets JSON containing `id`, `incident`, `service`, `status`, `text`
and `created_at`. Status is `firing` or `recovered`. An `Idempotency-Key` header
contains the notification ID. A 2xx response marks delivery as accepted. Failed
requests retry after a minute, in order; redirects are rejected. Only the operator
can configure the destination. Tool calls cannot choose a URL or send arbitrary text.

State, queued notifications and receipts persist in the agent's store. Delivery
is at least once: a crash after the receiver accepts a request can cause a retry
with the same ID. Receivers should deduplicate that ID. Delivered does not mean a
human read the message. The queue retains up to 100 notifications, removing old
delivered records first. If all are pending, new transitions cannot be recorded
until space is available; that error is logged. Run one Alert instance per deployment.

## Service

```sh
micro call alert Alert.List '{}'
```

Returns open incidents and recent delivery records, newest notifications first.
Polling and delivery use no model. The agent provides a read-only conversational
view; it cannot change routing, acknowledge incidents or repair services. See
[services/alert](../services/alert) for the implementation.
