# udp-to-metrics
Receive UDP messages and generate prometheus compatible metrics

## Daemon mode

```shell
udp-to-metrics -metricsFile metrics.json
```

The service listens for UDP messages on port `9090` and exposes Prometheus
metrics on `http://0.0.0.0:8080/metrics` by default. Use `-udpPort` and
`-httpPort` to change those ports.

## Supervise mode

```shell
udp-to-metrics -metricsFile metrics.json -supervise -superviseKillTimeout=30s -- /path/to/child arg1 arg2
```

Both listeners are bound before the child starts. The command after the
mandatory `--` is executed directly, without a shell. Its stdout and stderr are
connected to the corresponding streams of `udp-to-metrics`.

SIGINT and SIGTERM received by the supervisor are forwarded to the child's
process group. If the group is still alive after `-superviseKillTimeout`, it is
sent SIGKILL. The default timeout is 30 seconds; set it to `0` to disable forced
termination. When the child exits, the listeners close and the supervisor
returns the child's exit status (or terminates with the same signal).

Supervise mode supports Linux and macOS.

## Local test

The repository contains a small configuration in `examples/metrics.json`.
Start the listener:

```shell
go run ./cmd/udp-to-metrics -metricsFile examples/metrics.json
```

Send a counter update from another terminal:

```shell
printf '%s\n' '{"name":"utm_app_test_events","labels":["manual"],"command":"inc"}' | nc -u -w 1 127.0.0.1 9090
curl http://127.0.0.1:8080/metrics | grep utm_app_test_events
```

To exercise supervise mode, let a child shell send one metric and exit with a
visible status:

```shell
go run ./cmd/udp-to-metrics \
  -metricsFile examples/metrics.json \
  -supervise \
  -- /bin/sh -c 'printf "%s\n" "{\"name\":\"utm_app_test_events\",\"labels\":[\"supervise\"],\"command\":\"inc\"}" | nc -u -w 1 127.0.0.1 9090; echo child-finished'
```
