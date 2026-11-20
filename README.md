# spark for golem

An unofficial port of [spark](https://spark.lucko.me/), the performance
profiler, to [golem](https://github.com/golem-mc/golem), the Minecraft: Java
Edition server written in Go. It keeps spark's `/spark` command tree, flags,
permission nodes, messages and `config.json`, so an admin coming from Bukkit or
Paper finds the same plugin.

## Install

spark is a single module; nothing else is needed or optional.

```sh
golem-launcher plugin add github.com/golem-mc/spark
```

Restart the server. The first start creates `plugins/spark/`.

## Getting started

Run these as an operator in game or from the console:

```
/spark tps                         how the server is doing right now
/spark profiler start --timeout 60 sample the server for a minute
/spark profiler stop               finish early and get a link to the report
```

A profile ends with a link to spark's web viewer. `/spark profiler open` opens
a live view of the running profile, and `/spark health` uploads a health report and gives a link to it.
A background profiler runs from startup and keeps the last hour, so
`/spark profiler open` and `/spark health` have data straight away.

## Commands

`/spark` alone lists the commands you may use. Flags are written `--flag`;
tab completion offers every sub-command and flag.

### Profiler

| Command | What it does | Permission |
|---|---|---|
| `/spark profiler start [--timeout <seconds>] [--thread <name\|*>] [--only-ticks-over <ms>] [--interval <ms>] [--alloc] [--lock] [--regex] [--combine-all] [--not-combined]` | start sampling; `--alloc` profiles allocations (`--alloc-live-only` keeps only live ones), `--lock` lock contention | `spark.profiler` |
| `/spark profiler info` | show the running profile | `spark.profiler` |
| `/spark profiler open` | follow the running profile live in the browser | `spark.profiler` |
| `/spark profiler stop [--comment <text>] [--save-to-file]` | stop and upload the report (or save it) | `spark.profiler` |
| `/spark profiler cancel` | stop and discard the profile | `spark.profiler` |

Alias: `/spark sampler`.

### Server health

| Command | What it does | Permission |
|---|---|---|
| `/spark tps` | TPS, tick durations and CPU usage; replaces `/tps` (see `overrideTpsCommand`) | `spark.tps` |
| `/spark ping [--player <name>]` | players' ping now and over the last 15 minutes | `spark.ping` |
| `/spark health [dashboard\|upload]` | uploads a health report and gives a link; `dashboard`, the default, also keeps it live, `upload` does not | `spark.health` |
| `/spark health show [--memory] [--network]` | the health report in chat, or just its memory or network details | `spark.health` |
| `/spark tickmonitor [--threshold <percent>] [--threshold-tick <ms>] [--without-gc]` | toggle reports of ticks that run long | `spark.tickmonitor` |

Aliases: `/spark cpu` for `tps`, `healthreport` for `health`, `tickmonitoring`
for `tickmonitor`.

### Memory

| Command | What it does | Permission |
|---|---|---|
| `/spark gc` | garbage collection statistics | `spark.gc` |
| `/spark gcmonitor` | toggle a live report of every collection | `spark.gcmonitor` |
| `/spark heapsummary [--save-to-file] [--run-gc-before]` | what the heap holds, by allocation site | `spark.heapsummary` |
| `/spark heapdump [--compress gzip] [--run-gc-before] [--include-non-live]` | write a heap dump to `plugins/spark/` | `spark.heapdump` |

Alias: `/spark gcmonitoring` for `gcmonitor`.

### Activity

| Command | What it does | Permission |
|---|---|---|
| `/spark activity [--page <n>]` | the uploads and files spark made | `spark.activity` |

Aliases: `/spark activitylog`, `/spark log`.

## Permissions

| Node | Grants | Default |
|---|---|---|
| `spark` | every `/spark` command | operators |
| `spark.<command>` | one command: `spark.profiler`, `spark.tps`, `spark.ping`, `spark.health`, `spark.tickmonitor`, `spark.gc`, `spark.gcmonitor`, `spark.heapsummary`, `spark.heapdump`, `spark.activity` | operators |
| `bukkit.command.tps` | `/tps`, as on Paper | operators |

## Configuration

`plugins/spark/config.json` takes spark's keys; an environment variable
`SPARK_<KEY>` overrides each (for example `SPARK_BACKGROUNDPROFILER=false`).
Restart the server after editing.

| Key | Default | |
|---|---|---|
| `viewerUrl` | `https://spark.lucko.me/` | the web viewer links point to |
| `bytebinUrl` | `https://spark-usercontent.lucko.me/` | where reports are uploaded |
| `bytesocksHost` | `spark-usersockets.lucko.me` | the live viewer's relay |
| `trustedKeys` | `[]` | the web viewers trusted with `trust-viewer` |
| `overrideTpsCommand` | `true` | replace `/tps` with `/spark tps` |
| `disableResponseBroadcast` | `false` | send command output only to its sender |
| `backgroundProfiler` | `true` | run the background profiler |
| `backgroundProfilerInterval` | `10` | its sampling interval in milliseconds |

Output files (`profile-<date>.sparkprofile`, `.pprof`, heap summaries and
dumps, `activity.json`) are written to `plugins/spark/`.

## Migrating from Bukkit or Paper

Install the plugin and keep your habits: commands, flags, permission nodes
and `config.json` keys are spark's. Copy `config.json` from spark's plugin
folder to `plugins/spark/` to keep your settings. Grant the `spark.*` nodes with
your permissions plugin, or make players operators.

Reports open in the same web viewer as on Paper. For the commands' finer
points, see [spark's documentation](https://spark.lucko.me/docs/Command-Usage).

## Differences from spark

- Memory, garbage collection and heap figures are Go's, not the JVM's: there
  is one collector (`Go`), collections are timed by their pauses, and the
  maximum heap is `GOMEMLIMIT` or the machine's memory.
- The profiler sees only goroutines running on a CPU; sleeping or waiting
  ones never appear. `--ignore-sleeping` and `--force-java-sampler`
  (spark's options) are accepted and change nothing.
- Threads are shown by the function they started with; golem's tick loop is
  `Server thread`, and `--thread` takes those names.
- Only one CPU profile can run at once: set `backgroundProfiler` to `false`
  to use golem's `/perf` or `go tool pprof`.
- `--alloc` and `--lock` read the Go runtime's allocation and mutex profiles
  and cannot be combined with `--only-ticks-over`.
- `/spark heapsummary` lists allocation sites instead of classes, with
  estimated counts.
- `/spark heapdump` writes Go's own heap dump format, not hprof, so Eclipse
  MAT cannot open it; writing one stops the server for a moment.
- `/spark profiler stop --save-to-file` also writes a `.pprof` file for
  `go tool pprof`.
- Uploaded server configuration is `server.properties` only (without
  secrets).
- Java's `-Dspark.<key>` options do not exist; use `SPARK_<KEY>`.

## License

GPL-3.0, like spark, from which this is derived (spark is © lucko and
contributors). This project is not affiliated with or endorsed by spark.
