# spark for golem

An unofficial port of [spark](https://spark.lucko.me/), the performance
profiler, to [golem](https://github.com/golem-mc/golem), the Minecraft: Java
Edition server written in Go. It keeps spark's `/spark` command tree, flags,
permission nodes, messages and `config.json`, so an admin coming from Bukkit or
Paper finds the same plugin.

## What's included

- **`/spark tps`** (alias `cpu`) — TPS over the last 5s, 10s, 1m, 5m and 15m,
  tick durations (min/median/95th percentile/max) over 10s and 1m, and process
  and system CPU usage over 10s, 1m and 15m. `/tps` is replaced by the same
  report unless `overrideTpsCommand` is `false`.
- **`/spark ping`** — the online players' pings now and over the last 15
  minutes; `--player <name>` for one player.
- **`/spark health show`** (alias `healthreport`) — the report in chat: TPS,
  tick durations, CPU, memory, network and disk; `--memory` adds the runtime's
  non-heap memory and heap areas, `--network` every interface.
- **`/spark`**, **`/spark help`** and tab completion of every sub-command and
  flag.

## Not yet

- `/spark health` / `health upload` (the web viewer report), `/spark profiler`,
  `/spark tickmonitor`, `/spark gc`, `/spark gcmonitor`, `/spark heapsummary`,
  `/spark heapdump`, `/spark activity`, the background profiler and the live
  viewer are being ported.

## Installing

```sh
golem-launcher plugin add github.com/golem-mc/spark
```

The launcher pins the latest release (or a commit while there is none) in
`launcher.json` and builds the server with spark included.

### As a ready-built server

```sh
git clone https://github.com/golem-mc/spark.git
cd spark
go build -o spark-server ./cmd/server   # with mise: mise exec -- go build ...
./spark-server
```

## Configuration

spark writes nothing until it needs to; `plugins/spark/config.json` takes
spark's keys, and an environment variable `SPARK_<KEY>` (upper case) overrides
each, as on Java (system properties have no Go counterpart):

| Key | Default | |
|---|---|---|
| `overrideTpsCommand` | `true` | replace `/tps` with `/spark tps` |
| `disableResponseBroadcast` | `false` | send command output only to its sender, not to everyone with the permission |

## Permissions

`spark` grants every command; `spark.<command>` grants one, named by its first
alias: `spark.tps`, `spark.ping`, `spark.health`. All default to operators
(level 2). `/tps` also accepts `bukkit.command.tps`.

## Differences from spark on Bukkit

- **Memory** is the Go runtime's: the heap is the Go heap (live and unswept
  objects used, the heap spans held from the OS committed), and its maximum is
  `GOMEMLIMIT` when set, otherwise the machine's physical memory, since a Go
  heap has no `-Xmx`. `--memory` shows the runtime's non-heap memory (stacks,
  allocator metadata) and its heap areas in place of the JVM's memory pools.
- **Commands run on the tick goroutine**, not on a spark thread; anything slow
  (uploads, heap analysis) runs on its own goroutine and answers on the next
  tick.

## Development

See [PORTING.md](PORTING.md) for the layout and how the Java maps to Go, and
[CONTRIBUTING.md](CONTRIBUTING.md) for the checks and commit conventions.

## License

GPL-3.0, like spark, from which this is derived (spark is © lucko and
contributors). This project is not affiliated with or endorsed by spark.
