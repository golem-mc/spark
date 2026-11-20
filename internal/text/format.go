package text

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/golem-mc/golem/plugin"
)

var sizeUnits = []string{"bytes", "KB", "MB", "GB", "TB", "PB", "EB", "ZB", "YB"}

// Percent is value as a whole percentage of max, truncated. spark: FormatUtil.percent
func Percent(value, max float64) string {
	return strconv.Itoa(int(value*100/max)) + "%"
}

func sizeIndex(bytes int64) int {
	return int(math.Log(float64(bytes)) / math.Log(1024))
}

// Bytes is a byte count in the largest binary unit, to one decimal.
// spark: FormatUtil.formatBytes
func Bytes(bytes int64) string {
	if bytes <= 0 {
		return "0 bytes"
	}
	i := sizeIndex(bytes)
	return fmt.Sprintf("%.1f %s", float64(bytes)/math.Pow(1024, float64(i)), sizeUnits[i])
}

// BytesColored is Bytes with the number coloured and suffix after the unit.
func BytesColored(bytes int64, color, suffix string) plugin.Text {
	value, unit := "0", "KB"+suffix
	if bytes > 0 {
		i := sizeIndex(bytes)
		value = fmt.Sprintf("%.1f", float64(bytes)/math.Pow(1024, float64(i)))
		unit = sizeUnits[i] + suffix
	}
	return Of(T(value, color), Space(), T(unit))
}

// Seconds is a duration as "Xm Ys". spark: FormatUtil.formatSeconds
func Seconds(seconds int64) string {
	if seconds <= 0 {
		return "0s"
	}
	var sb strings.Builder
	if m := seconds / 60; m != 0 {
		fmt.Fprintf(&sb, "%dm ", m)
	}
	if s := seconds % 60; s != 0 {
		fmt.Fprintf(&sb, "%ds ", s)
	}
	return strings.TrimSpace(sb.String())
}

// JavaDouble formats f as Java's Double.toString does for the values spark
// prints: the shortest form, with ".0" on a whole number.
func JavaDouble(f float64) string {
	s := strconv.FormatFloat(f, 'f', -1, 64)
	if !strings.Contains(s, ".") && !math.IsInf(f, 0) && !math.IsNaN(f) {
		s += ".0"
	}
	return s
}

// Decimal2 formats f as Java's DecimalFormat("#.##"): at most two decimals,
// no trailing zeros.
func Decimal2(f float64) string {
	s := strconv.FormatFloat(f, 'f', 2, 64)
	s = strings.TrimRight(s, "0")
	return strings.TrimSuffix(s, ".")
}

// TPS colours a ticks-per-second value against the target rate.
// spark: StatisticFormatter.formatTps
func TPS(tps float64, target int) plugin.Text {
	color := Red
	switch {
	case tps > float64(target)*0.9:
		color = Green
	case tps > float64(target)*0.8:
		color = Yellow
	}
	star := ""
	if tps > float64(target) {
		star = "*"
	}
	return T(star+JavaDouble(math.Min(math.Round(tps*100)/100, float64(target))), color)
}

// Averages is the min, median, 95th percentile and max of a series.
type Averages interface {
	Mean() float64
	Max() float64
	Min() float64
	Median() float64
	Percentile95() float64
}

// TickDurations is "min/med/95%ile/max" coloured against the longest ideal
// tick. spark: StatisticFormatter.formatTickDurations
func TickDurations(a Averages, maxIdeal int) plugin.Text {
	return Of(
		TickDuration(a.Min(), maxIdeal), T("/", Gray),
		TickDuration(a.Median(), maxIdeal), T("/", Gray),
		TickDuration(a.Percentile95(), maxIdeal), T("/", Gray),
		TickDuration(a.Max(), maxIdeal),
	)
}

// TickDuration colours one tick duration in milliseconds.
func TickDuration(d float64, maxIdeal int) plugin.Text {
	color := Green
	switch {
	case d >= float64(maxIdeal):
		color = Red
	case d >= float64(maxIdeal)*0.8:
		color = Yellow
	}
	return T(fmt.Sprintf("%.1f", d), color)
}

// CPU colours a CPU usage fraction (0 to 1) as a percentage.
// spark: StatisticFormatter.formatCpuUsage
func CPU(usage float64) plugin.Text {
	color := Green
	switch {
	case usage > 0.9:
		color = Red
	case usage > 0.65:
		color = Yellow
	}
	return T(Percent(usage, 1), color)
}

// PingRTTs is "min/med/95%ile/max" of round trip times in milliseconds.
func PingRTTs(min, median, p95, max float64) plugin.Text {
	return Of(PingRTT(min), T("/", Gray), PingRTT(median), T("/", Gray), PingRTT(p95), T("/", Gray), PingRTT(max))
}

// PingRTT colours one round trip time in milliseconds, rounded up.
// spark: StatisticFormatter.formatPingRtt
func PingRTT(ping float64) plugin.Text {
	color := Green
	switch {
	case ping >= 200:
		color = Red
	case ping >= 100:
		color = Yellow
	}
	return T(strconv.Itoa(int(math.Ceil(ping))), color)
}

const (
	barTrue  = "┃"
	barFalse = "╻"
)

func bar(n int, color string) plugin.Text {
	return T(strings.Repeat(barTrue, max(n, 0)), color)
}

func gap(n int, color string) plugin.Text {
	return T(strings.Repeat(barFalse, max(n, 0)), color)
}

func chars(v, max float64, length int) int { return int(v * float64(length) / max) }

// MemoryDiagram is a bar of used and committed memory against max.
// spark: StatisticFormatter.generateMemoryUsageDiagram
func MemoryDiagram(used, committed, max float64, length int) plugin.Text {
	usedChars := chars(used, max, length)
	committedChars := chars(committed, max, length)
	line := bar(usedChars, Yellow)
	if committedChars > usedChars {
		line.Children = append(line.Children, gap(committedChars-usedChars-1, Gray), T(barFalse, Red))
	}
	if length > committedChars {
		line.Children = append(line.Children, gap(length-committedChars, Gray))
	}
	return Of(T("[", DarkGray), line, T("]", DarkGray))
}

// MemoryPoolDiagram is MemoryDiagram with the usage after the last
// collection marked. spark: StatisticFormatter.generateMemoryPoolDiagram
func MemoryPoolDiagram(used, collectionUsed, committed, max float64, length int) plugin.Text {
	usedChars := chars(used, max, length)
	collectionChars := chars(collectionUsed, max, length)
	committedChars := chars(committed, max, length)
	line := bar(collectionChars, Yellow)
	if usedChars > collectionChars {
		line.Children = append(line.Children, T(barTrue, Red), bar(usedChars-collectionChars-1, Yellow))
	}
	if committedChars > usedChars {
		line.Children = append(line.Children, gap(committedChars-usedChars-1, Gray), T(barFalse, Yellow))
	}
	if length > committedChars {
		line.Children = append(line.Children, gap(length-committedChars, Gray))
	}
	return Of(T("[", DarkGray), line, T("]", DarkGray))
}

// DiskDiagram is a bar of used disk space. spark: StatisticFormatter.generateDiskUsageDiagram
func DiskDiagram(used, max float64, length int) plugin.Text {
	usedChars := chars(used, max, length)
	return Of(T("[", DarkGray), bar(usedChars, Yellow), gap(length-usedChars, Gray), T("]", DarkGray))
}
