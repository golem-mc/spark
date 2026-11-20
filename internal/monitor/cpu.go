package monitor

import (
	"os"
	"runtime"
	"time"

	"github.com/golem-mc/spark/internal/rolling"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/process"
)

// CPU is the process's and the machine's CPU usage, as fractions of all
// CPUs (0 to 1), polled every second. spark: CpuMonitor
type CPU struct {
	system10s, system1m, system15m    *rolling.Average
	process10s, process1m, process15m *rolling.Average

	proc *process.Process

	lastWall          time.Time
	lastProcess       float64 // seconds of CPU the process has used
	lastBusy, lastAll float64 // seconds of CPU the machine has used, and in all
}

func newCPU() *CPU {
	c := &CPU{
		system10s: rolling.New(10), system1m: rolling.New(60), system15m: rolling.New(60 * 15),
		process10s: rolling.New(10), process1m: rolling.New(60), process15m: rolling.New(60 * 15),
	}
	c.proc, _ = process.NewProcess(int32(os.Getpid()))
	c.lastWall = time.Now()
	c.lastProcess = c.processSeconds()
	c.lastBusy, c.lastAll = systemSeconds()
	return c
}

func (c *CPU) processSeconds() float64 {
	if c.proc == nil {
		return -1
	}
	t, err := c.proc.Times()
	if err != nil {
		return -1
	}
	return t.User + t.System
}

// systemSeconds answers the machine's busy and total CPU seconds; iowait
// counts as idle, as in the JDK's getSystemCpuLoad.
func systemSeconds() (busy, all float64) {
	times, err := cpu.Times(false)
	if err != nil || len(times) == 0 {
		return -1, -1
	}
	t := times[0]
	all = t.User + t.System + t.Idle + t.Nice + t.Iowait + t.Irq + t.Softirq + t.Steal
	return all - t.Idle - t.Iowait, all
}

// poll measures the last second; a reading the OS cannot give is skipped.
func (c *CPU) poll() (system, process float64) {
	now := time.Now()
	wall := now.Sub(c.lastWall).Seconds()
	proc := c.processSeconds()
	busy, all := systemSeconds()
	system, process = -1, -1
	if all > c.lastAll && c.lastAll >= 0 && busy >= 0 {
		system = min(1, max(0, (busy-c.lastBusy)/(all-c.lastAll)))
	}
	if wall > 0 && proc >= 0 && c.lastProcess >= 0 {
		process = min(1, max(0, (proc-c.lastProcess)/(wall*float64(runtime.NumCPU()))))
	}
	c.lastWall, c.lastProcess, c.lastBusy, c.lastAll = now, proc, busy, all
	if system >= 0 {
		c.system10s.Add(system)
		c.system1m.Add(system)
		c.system15m.Add(system)
	}
	if process >= 0 {
		c.process10s.Add(process)
		c.process1m.Add(process)
		c.process15m.Add(process)
	}
	return system, process
}

func (c *CPU) System10Sec() float64  { return c.system10s.Mean() }
func (c *CPU) System1Min() float64   { return c.system1m.Mean() }
func (c *CPU) System15Min() float64  { return c.system15m.Mean() }
func (c *CPU) Process10Sec() float64 { return c.process10s.Mean() }
func (c *CPU) Process1Min() float64  { return c.process1m.Mean() }
func (c *CPU) Process15Min() float64 { return c.process15m.Mean() }
