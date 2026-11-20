# spark for golem

An unofficial port of [spark](https://spark.lucko.me/), the performance
profiler, to [golem](https://github.com/golem-mc/golem), the Minecraft: Java
Edition server written in Go. It keeps spark's `/spark` command tree, flags,
permission nodes, messages and `config.json`, so an admin coming from Bukkit or
Paper finds the same plugin.

## What's included

- **`/spark profiler`** (alias `sampler`) — `start` samples the server and
  `stop` uploads the profile to bytebin and prints its link in spark's web
  viewer (`upload` is an alias); `info` shows the running profiler and
  `cancel` drops it. `start` takes spark's flags: `--timeout <seconds>` stops
  and uploads by itself, `--interval <ms>` sets the sampling interval
  (default 10ms, the Go runtime's own rate), `--thread <name>` picks
  goroutines (`--thread *` all of them, `--regex` to match names),
  `--combine-all`/`--not-combined` change how they are grouped, and
  `--only-ticks-over <ms>` keeps only the ticks that took at least that long.
  `--alloc` profiles memory allocation (`--alloc-live-only` the bytes still
  in use) and `--lock` the time goroutines wait for locks.
  As in spark, a **background profiler** runs from startup and keeps the
  last hour: `/spark profiler stop` uploads it, `start` replaces it with the
  profiler asked for, and it starts again after a `stop`.
  `stop` takes `--comment <text>`, `--separate-parent-calls`, and
  `--save-to-file`, which writes `plugins/spark/profile-<date>.sparkprofile`
  (open it in the viewer) instead of uploading. Golem's tick goroutine is the
  viewer's **Server thread**, and each frame is attributed to the plugin whose
  Go module holds its package. With `--save-to-file`, spark also writes the
  same samples, every goroutine included, as `profile-<date>.pprof` for
  `go tool pprof` — a golem extra.
- **The live viewer.** `/spark profiler open` links to the running profile
  in spark's web viewer, which follows it through a bytesocks channel: a new
  profile every minute and fresh statistics every 10 seconds. The first time
  a browser connects, the viewer shows the command that trusts it:
  `/spark profiler trust-viewer --id <id>` (the key is kept in `trustedKeys`).
- **`/spark tps`** (alias `cpu`) — TPS over the last 5s, 10s, 1m, 5m and 15m,
  tick durations (min/median/95th percentile/max) over 10s and 1m, and process
  and system CPU usage over 10s, 1m and 15m. `/tps` is replaced by the same
  report unless `overrideTpsCommand` is `false`.
- **`/spark ping`** — the online players' pings now and over the last 15
  minutes; `--player <name>` for one player.
- **`/spark health`** (alias `healthreport`) — `dashboard` (the default)
  uploads a health report and keeps it live in the viewer, as the profiler's
  live viewer does; `upload` uploads a health report
  to bytebin and prints its link in spark's web viewer: platform, system,
  memory, GC, TPS/MSPT, ping, players, per-world and per-chunk entity counts, game rules and data packs,
  `server.properties` (without secrets such as `rcon.password` and
  `level-seed`), the plugins, and the last hour of TPS, tick duration, CPU,
  memory, allocation, world and ping graphs. `show` prints the report in chat;
  `--memory` adds the runtime's non-heap memory and heap areas, `--network`
  every interface.
- **`/spark tickmonitor`** (alias `tickmonitoring`) — times the ticks for 120
  ticks, then reports every tick more than 100% longer than that average;
  `--threshold <percent>` changes the percentage, `--threshold-tick <ms>`
  reports ticks longer than a duration instead, and collections are reported
  too unless `--without-gc`. Run it again to stop.
- **`/spark gc`** — the collector's average pause, collections and how often
  they run since the server started. **`/spark gcmonitor`** (alias
  `gcmonitoring`) toggles a report of every collection: its pause, whether it
  was forced, and the heap it freed.
- **`/spark heapsummary`** — uploads the heap in use to the viewer (or saves
  `plugins/spark/heapsummary-<date>.sparkheap` with `--save-to-file`), grouped
  by where the objects were allocated; `--run-gc-before` collects first.
- **`/spark heapdump`** — writes `plugins/spark/heap-<date>.heapdump`;
  `--compress gzip` also writes a `.gz`, and `--include-non-live` skips the
  collection that otherwise leaves only live objects.
- **`/spark activity`** (`activitylog`, `log`) — the uploads and files spark
  made, `--page <n>`, kept in `plugins/spark/activity.json`.
- **`/spark`**, **`/spark help`** and tab completion of every sub-command and
  flag.

## Not yet

Every command, sub-command and flag of spark's is ported. Some behave
differently on Go's runtime; see [Differences from spark on Bukkit](#differences-from-spark-on-bukkit).

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
| `viewerUrl` | `https://spark.lucko.me/` | the web viewer links point to |
| `bytebinUrl` | `https://spark-usercontent.lucko.me/` | where reports are uploaded |
| `bytesocksHost` | `spark-usersockets.lucko.me` | the live viewer's relay, over TLS; a URL such as `http://localhost:8080` reaches one without |
| `trustedKeys` | `[]` | the web viewers trusted with `trust-viewer` |
| `overrideTpsCommand` | `true` | replace `/tps` with `/spark tps` |
| `disableResponseBroadcast` | `false` | send command output only to its sender, not to everyone with the permission |
| `backgroundProfiler` | `true` | run the background profiler; spark writes `true` here the first time |
| `backgroundProfilerInterval` | `10` | its sampling interval in milliseconds |
| `backgroundProfilerThreadGrouper` | `by-pool` | `by-pool`, `by-name` or `as-one` |
| `backgroundProfilerThreadDumper` | `default` | `default` (the server thread), `all`, or goroutine names separated by commas |

`backgroundProfilerEngine` is not read: Go has one profiling engine.

## Permissions

`spark` grants every command; `spark.<command>` grants one, named by its first
alias: `spark.profiler`, `spark.tps`, `spark.ping`, `spark.health`,
`spark.tickmonitor`, `spark.gc`, `spark.gcmonitor`, `spark.heapsummary`,
`spark.heapdump`, `spark.activity`. All default to operators
(level 2). `/tps` also accepts `bukkit.command.tps`.

## Differences from spark on Bukkit

- **Memory** is the Go runtime's: the heap is the Go heap (live and unswept
  objects used, the heap spans held from the OS committed), and its maximum is
  `GOMEMLIMIT` when set, otherwise the machine's physical memory, since a Go
  heap has no `-Xmx`. `--memory` shows the runtime's non-heap memory (stacks,
  allocator metadata) and its heap areas in place of the JVM's memory pools.
- **The runtime in uploads.** The viewer's "Java" and "JVM" fields hold the Go
  version and toolchain, its VM arguments the `GOGC`/`GOMEMLIMIT`/
  `GOMAXPROCS`/`GODEBUG` settings and the server's flags. The platform is
  named `golem`, with golem's module version. GC figures are the Go
  collector's (named `Go`): collections and their stop-the-world pauses.
- **Garbage collection.** Go has one concurrent collector, reported as `Go`
  where a JVM has young and old generations. A collection's duration is its
  stop-the-world pauses, usually well under a millisecond. Its cause is
  `forced` for a `runtime.GC` and left out otherwise, and the heap before it is
  estimated from the previous collection's live heap plus what was allocated
  since.
- **Heap summary.** Go objects carry no class, so the summary lists
  allocation sites (`github.com/golem-mc/golem/internal/world.(*Chunk).load`)
  where spark lists classes. It comes from the runtime's heap profile, which
  samples about one allocation per 512 KiB, so its counts are estimates.
- **Heap dump** files are in the Go runtime's own format
  (`runtime/debug.WriteHeapDump`), not hprof, so JVM tools such as Eclipse MAT
  cannot open them. Writing one stops the server for its duration, as on Java.
  As in spark, `gzip` is the only compression.
- **The profiler** is the Go runtime's CPU profiler, not async-profiler or a
  thread dumper, so it sees only goroutines that are running on a CPU:
  waiting and sleeping goroutines never appear (as with spark's
  `--ignore-sleeping`, which is accepted and changes nothing), and neither
  does `--force-java-sampler`. The viewer shows it as the async engine.
  Goroutines have no names: a goroutine is shown under the function it was
  started with (`server.(*Server).acceptLoop`), golem's tick goroutine as
  `Server thread`, and `--thread` takes those names. A frame's class is its Go
  package with the method's receiver (`github.com/golem-mc/golem/internal/server.(*Server)`)
  and its method the rest (`tickLoop`). An `--interval` other than 10ms makes
  the Go runtime print `cannot set cpu profile rate until previous profile has
  finished` once per profile; the rate is set all the same. Only one CPU
  profile can run in a process, so the profiler cannot start while golem's
  `/perf` or anything else is profiling, and the other way round. The
  background profiler holds it whenever no other profiler runs: set
  `backgroundProfiler` to `false` to use `/perf` or `go tool pprof`'s
  `/debug/pprof/profile`.
- **`--alloc`** reads the runtime's allocation profile, which samples about
  one allocation per 512 KiB (`runtime.MemProfileRate`, so `--interval`
  does not apply) and takes in allocations at each garbage collection; the
  profiler collects garbage once as it stops, to count the latest ones.
  `--alloc-live-only` shows how much the bytes in use grew, by where they
  were allocated. **`--lock`** reads the mutex profile: the time goroutines
  waited for a `sync.Mutex` or `RWMutex`, every contention recorded and
  charged to the code that held the lock (channel waits are not locks
  here). Neither profile records which tick a sample belongs to, so
  `--only-ticks-over` cannot be combined with them, and goroutines are
  recognised by their entry function alone.
- **Commands run on the tick goroutine**, not on a spark thread; anything slow
  (uploads, heap analysis) runs on its own goroutine and answers on the next
  tick.

## Development

See [PORTING.md](PORTING.md) for the layout and how the Java maps to Go, and
[CONTRIBUTING.md](CONTRIBUTING.md) for the checks and commit conventions.

## License

GPL-3.0, like spark, from which this is derived (spark is © lucko and
contributors). This project is not affiliated with or endorsed by spark.
