package text

import (
	"testing"

	"github.com/golem-mc/spark/internal/rolling"
)

func TestFormatting(t *testing.T) {
	cases := []struct{ got, want string }{
		{Bytes(0), "0 bytes"},
		{Bytes(512), "512.0 bytes"},
		{Bytes(1536), "1.5 KB"},
		{Bytes(3 << 30), "3.0 GB"},
		{Percent(1, 3), "33%"},
		{Seconds(0), "0s"},
		{Seconds(59), "59s"},
		{Seconds(60), "1m"},
		{Seconds(125), "2m 5s"},
		{JavaDouble(20), "20.0"},
		{JavaDouble(19.53), "19.53"},
		{Decimal2(12.5), "12.5"},
		{Decimal2(3.14159), "3.14"},
		{Decimal2(7), "7"},
		{TPS(20.4, 20).PlainString(), "*20.0"},
		{TPS(18.456, 20).PlainString(), "18.46"},
		{TPS(18.456, 20).Style.Color, Green},
		{TPS(17, 20).Style.Color, Yellow},
		{TPS(19, 20).Style.Color, Green},
		{TPS(10, 20).Style.Color, Red},
		{CPU(0.7).PlainString(), "70%"},
		{CPU(0.7).Style.Color, Yellow},
		{PingRTT(99.2).PlainString(), "100"},
		{PingRTT(99.2).Style.Color, Green},
		{PingRTT(100).Style.Color, Yellow},
		{TickDuration(50, 50).Style.Color, Red},
		{TickDurations(rolling.Values{MinV: 1, MedianV: 2, P95: 3, MaxV: 41}, 50).PlainString(), "1.0/2.0/3.0/41.0"},
		{BytesColored(0, Green, "/s").PlainString(), "0 KB/s"},
	}
	for i, c := range cases {
		if c.got != c.want {
			t.Errorf("case %d: got %q, want %q", i, c.got, c.want)
		}
	}
}

func TestDiagrams(t *testing.T) {
	d := MemoryDiagram(25, 50, 100, 10).PlainString()
	if d != "[┃┃╻╻╻╻╻╻╻╻]" {
		t.Errorf("memory diagram = %q", d)
	}
	if d := DiskDiagram(30, 100, 10).PlainString(); d != "[┃┃┃╻╻╻╻╻╻╻]" {
		t.Errorf("disk diagram = %q", d)
	}
	if d := MemoryPoolDiagram(40, 20, 60, 100, 10).PlainString(); d != "[┃┃┃┃╻╻╻╻╻╻]" {
		t.Errorf("pool diagram = %q", d)
	}
}
