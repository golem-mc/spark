# Porting spark to golem

This module is spark (`spark-common` and `spark-bukkit`/`spark-paper`, Java)
ported to a golem plugin. It ports behaviour, not the Java's structure: one
`internal/platform` package holds what `SparkPlatform`, `CommandManager` and the
command modules do, and small leaf packages hold the rest.

The Java reference is spark's repository; clone it into the ignored `spark/`
directory to read alongside (`git clone https://github.com/lucko/spark spark`).

## Layout

| Path | What | Java counterpart |
|---|---|---|
| `spark.go` | The `spark` plugin: builds and enables the platform | `BukkitSparkPlugin` |
| `internal/platform/` | The platform, `/spark` and its argument parsing, completion and replies, and one `module_*.go` per command module | `SparkPlatform`, `CommandManager`, `Arguments`, `CommandResponseHandler`, `TabCompleter`, `command/modules/*` |
| `internal/monitor/` | Tick rate and durations, CPU, memory, network, disk and ping monitors | `monitor/*` |
| `internal/config/` | `config.json` with `SPARK_*` overrides | `util/config/*` |
| `internal/text/` | Chat components, spark's prefix, and number and bar formatting | adventure calls, `FormatUtil`, `StatisticFormatter` |
| `internal/rolling/` | Rolling averages | `RollingAverage` |
| `cmd/server/` | A server binary with spark built in | — |

## How the Java maps to Go

- **A command module** is a `func(*Platform) []*Command` in
  `internal/platform/module_<name>.go`, listed in `New` in spark's module
  order (which is the order `/spark help` shows). A `Command` carries the
  aliases (the first names its permission `spark.<alias>`), the usage flags,
  whether it takes a sub-command, `Run` and `Complete`.
- **Arguments** parse as in Java: an optional sub-command, then `--flag
  value words`. `Int` and `Float` answer `-1` for an absent flag and a
  `ParseError` for a bad one, which the manager shows in red.
- **Replies.** `Responder.Reply`/`ReplyPrefixed` answer the sender,
  `Broadcast`/`BroadcastPrefixed` everyone with the command's permission plus
  the console. A Responder is for the tick goroutine; work moved to another
  goroutine takes `r.Async()`, which delivers on the next tick.
- **Messages** are built with `internal/text`: `text.T("x", text.Gray)` is
  adventure's `text("x", GRAY)`, `text.Of`/`text.Colored` its builders, and
  `text.Prefix` spark's `[⚡]` prefix. Keep spark's wording and colours.
- **Monitors** that spark runs as static singletons with their own executor
  live on the `Platform` and stop in `Disable`: tick statistics are fed by
  golem's `ServerTickStartEvent`/`ServerTickEndEvent` (Paper's tick events),
  CPU is polled every second and the network every minute
  (`monitor.Monitors`), pings every 10 seconds.

## Deliberate differences

- **Memory** is the Go runtime's (`runtime/metrics`): see the README.
- **System properties.** Java's `-Dspark.<key>` overrides have no Go
  counterpart; the `SPARK_<KEY>` environment variables remain.
- **Command execution.** spark runs every command on its own thread behind a
  lock; here `Run` is on the tick goroutine (so it may read players and worlds)
  and moves only slow work off it.
