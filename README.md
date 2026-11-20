# spark for golem

An unofficial port of [spark](https://spark.lucko.me/), the performance
profiler, to [golem](https://github.com/golem-mc/golem), the Minecraft: Java
Edition server written in Go. It keeps spark's `/spark` command tree, flags,
permission nodes, messages and `config.json`, so an admin coming from Bukkit or
Paper finds the same plugin. Command usage is in
[spark's documentation](https://spark.lucko.me/docs/Command-Usage).

```sh
golem-launcher plugin add github.com/golem-mc/spark
```

## What's included

- **`/spark profiler`**: sample the server, then upload the profile to spark's
  web viewer or save it to a file.
- **A background profiler** that runs from startup and keeps the last hour.
- **A live viewer**: `/spark profiler open` follows the running profile in the
  browser.
- **`/spark tps`**: TPS, tick durations and CPU usage; replaces `/tps`.
- **`/spark ping`**: players' ping now and over the last 15 minutes.
- **`/spark health`**: a health report in the web viewer or in chat.
- **`/spark tickmonitor`**: reports ticks that run long.
- **`/spark gc`** and **`/spark gcmonitor`**: garbage collection statistics and
  a live report of every collection.
- **`/spark heapsummary`** and **`/spark heapdump`**: heap analysis and a heap
  dump.
- **`/spark activity`**: the uploads and files spark made.
- **Tab completion** of every sub-command and flag.

On Go's runtime, memory, garbage collection, heap and profiler output differ
from the JVM's; see [PORTING.md](PORTING.md).

## Not yet

Nothing: every command, sub-command and flag of spark's is ported.

## Configuration

`plugins/spark/config.json` takes spark's keys; an environment variable
`SPARK_<KEY>` overrides each.

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

## Permissions

`spark` grants every command and `spark.<command>` one (`spark.profiler`,
`spark.tps`, ...). All default to operators.

## Development

See [PORTING.md](PORTING.md) and [CONTRIBUTING.md](CONTRIBUTING.md).

## License

GPL-3.0, like spark, from which this is derived (spark is © lucko and
contributors). This project is not affiliated with or endorsed by spark.
