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
| `internal/viewer/` | The live viewer's bytesocks channel, signed packets and trusted keys; `viewertest` fakes bytesocks and the web viewer | `ws/*`, bytesocks-java-client |
| `internal/sampler/` | The profiler: CPU profiles, the call trees, threads and their grouping, the viewer's export | `sampler/*` |
| `internal/monitor/` | Tick rate and durations, CPU, memory, network, disk and ping monitors | `monitor/*` |
| `internal/metric/` | The last hour of time series for the viewer's graphs | `metric/*` |
| `internal/sparkproto/` | spark's protobuf messages, generated from `proto/spark/*.proto` (spark's files, unchanged) by `mise run gen:proto` | `src/main/proto` |
| `internal/bytebin/` | Uploads to bytebin | `BytebinClient` |
| `internal/activitylog/` | `activity.json` | `activitylog/*` |
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

- **Uploads.** A report gathers what only the tick goroutine may read
  (players, worlds) with `p.snapshot(world)` in `Run`, then builds the
  message, uploads it (`p.upload`) and announces the link on another
  goroutine. `gatherMetadata` is spark's `SparkMetadata.gather`; the
  statistics it fills are in `statistics.go` and `world.go`.

## GC notifications

The JVM notifies listeners of each collection; Go does not.
`monitor.GCMonitor` attaches a `runtime.AddCleanup` to an object nothing
references. The cleanup runs after the collection that frees it, reads the
collection from `debug.ReadGCStats` and `runtime/metrics`, and attaches
itself to a new object. A notice that covers several cycles reports each one.

## The sampler

spark samples Java threads' stacks itself (or through async-profiler); Go
cannot read another goroutine's stack at an interval, so `internal/sampler`
uses the runtime's CPU profiler (`runtime/pprof`), which interrupts running
threads with `SIGPROF` and records the goroutine's stack and labels.

- **Windows.** The profile is stopped and restarted at each one-minute window
  boundary (and when a profile is exported), parsed with
  `github.com/google/pprof/profile`, and its samples added to the trees under
  that window, so the viewer's per-window statistics and its time slider work
  as on Java. Windows are shifted by a random offset and pruned to the last 60,
  as spark does. With a non-default `--interval` the profile is not rotated
  each minute, so the runtime's notice about the rate is printed once.
- **The Server thread.** On each `ServerTickStartEvent` the tick goroutine
  labels itself (`pprof.SetGoroutineLabels`) as `spark-thread=Server thread`,
  and learns its entry function (`runtime.Callers`) the first time. Labels are
  inherited by goroutines started from the tick goroutine, so a sample is the
  server thread's when its outermost frame is that entry function; the label
  decides only for a stack the runtime cut off at 64 frames.
- **Other goroutines** are named by their entry function without its
  package's directory, and grouped by spark's thread groupers.
- **`--only-ticks-over`** adds the tick number to the label. Samples wait
  until `ServerTickEndEvent` says how long their tick took, and are kept, in
  the window the tick ended in, only when it took at least the threshold.
  Samples outside a tick are dropped.
- **Plugins.** A frame's class is its package (plus receiver), so the class
  sources map each package to the plugin with the longest `plugin.Info.Module`
  that holds it.
- **Modes.** A sampler reads a `source`: the CPU profiler, restarted at each
  read, or, for `--alloc` and `--lock`, the runtime's cumulative `allocs` or
  `mutex` profile, from which each read takes what every stack gained since
  the last one. Trees hold microseconds of CPU, bytes, or nanoseconds of
  delay, and the export scales them to what the viewer reads
  (spark: `SamplerMode`'s value transformer).
- **The background profiler** is an ordinary sampler marked `Background`,
  started in `Enable` and again after `/spark profiler stop`. It runs for as
  long as the server does; pruning to 60 windows bounds its trees.
- **Locks.** The tick goroutine takes only `tickMu`, a short lock on tick
  counts; parsing a profile happens on the sampler's goroutine under `mu`, so
  it never holds a tick up.

## The live viewer

A socket is a bytesocks channel (`GET /create`, then a websocket to the
channel) on which spark's `RawPacket`s travel as base64 text, signed with
the server's RSA-2048 key (SHA256withRSA). The payload uploaded to bytebin
carries the channel and the server's public key. A sampler's sockets are
sent a new profile from `sampler.Env.OnWindowRotate` and close in `OnStop`.
Statistics are gathered on the tick goroutine and sent from another one.

## Deliberate differences

- **Memory** is the Go runtime's (`runtime/metrics`): see the README.
- **System properties.** Java's `-Dspark.<key>` overrides have no Go
  counterpart; the `SPARK_<KEY>` environment variables remain.
- **Allocation rate averages** span 1, 5 and 15 minutes of 10-second
  samples; spark keeps 60, 300 and 900 samples, which is 10 times longer than
  their names say.
- **Server configurations.** Only `server.properties` is uploaded: golem has
  no `bukkit.yml`, `spigot.yml` or `paper/`.
- **The sampler engine** is reported as async (it is the runtime's
  signal-based profiler) with the Go version; `--force-java-sampler` and
  `--ignore-sleeping` are accepted and have no effect: see the README.
- **Command execution.** spark runs every command on its own thread behind a
  lock; here `Run` is on the tick goroutine (so it may read players and worlds)
  and moves only slow work off it.
