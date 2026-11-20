package platform

import (
	"os"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/spark/internal/activitylog"
	"github.com/golem-mc/spark/internal/bytebin"
	"github.com/golem-mc/spark/internal/heap"
	"github.com/golem-mc/spark/internal/sparkproto"
	"github.com/golem-mc/spark/internal/text"
)

func heapCommands(*Platform) []*Command {
	return []*Command{
		{
			Aliases: []string{"heapsummary"},
			Usage:   []ArgInfo{{Name: "save-to-file"}},
			Run:     heapSummary,
			Complete: func(_ *Platform, _ plugin.CommandSender, args []string) []string {
				return CompleteFlags(args, "--save-to-file", "--run-gc-before")
			},
		},
		{
			Aliases: []string{"heapdump"},
			Usage:   []ArgInfo{{Name: "compress", Param: "type"}},
			Run:     heapDump,
			Complete: func(_ *Platform, _ plugin.CommandSender, args []string) []string {
				return CompleteFlags(args, "--compress", "--run-gc-before", "--include-non-live")
			},
		},
	}
}

// heapSummary uploads or saves the heap in use by allocation site.
// spark: HeapAnalysisModule.heapSummary
func heapSummary(p *Platform, _ plugin.CommandSender, r *Responder, args *Arguments) error {
	runGC := args.Bool("run-gc-before")
	saveToFile := args.Bool("save-to-file")
	snap := p.snapshot(true)
	async := r.Async()
	if runGC {
		r.BroadcastPrefixed(text.T("Running garbage collector..."))
	}
	r.BroadcastPrefixed(text.T("Creating a new heap dump summary, please wait..."))
	p.Server.Scheduler().RunAsync(func() {
		if runGC {
			runtime.GC()
		}
		entries, err := heap.Summary()
		if err != nil {
			async.BroadcastPrefixed(text.T("An error occurred whilst inspecting the heap.", text.Red))
			p.Server.Logger().Error("An error occurred whilst inspecting the heap.", "error", err)
			return
		}
		data := &sparkproto.HeapData{Metadata: p.gatherMetadata(async.Sender(), p.startupGC, snap).heap()}
		for _, e := range entries {
			data.Entries = append(data.Entries, &sparkproto.HeapEntry{Order: int32(e.Order), Instances: int32(min(e.Instances, 1<<31-1)), Size: e.Bytes, Type: e.Type})
		}
		save := saveToFile
		if !save {
			url, err := p.upload(data, bytebin.HeapMediaType, "")
			if err == nil {
				// spark's own spelling.
				async.BroadcastPrefixed(text.T("Heap dump summmary output:", text.Gold))
				async.Broadcast(text.OpenURL(text.T(url, text.Gray), url))
				p.logActivity(async, "Heap dump summary", activitylog.DataURL, url)
				return
			}
			async.BroadcastPrefixed(text.T("An error occurred whilst uploading the data. Attempting to save to disk instead.", text.Red))
			p.Server.Logger().Error("An error occurred whilst uploading the data.", "error", err)
			save = true
		}
		if save {
			file, err := p.saveProto("heapsummary", "sparkheap", data)
			if err != nil {
				async.BroadcastPrefixed(text.T("An error occurred whilst saving the data.", text.Red))
				p.Server.Logger().Error("An error occurred whilst saving the data.", "error", err)
				return
			}
			async.BroadcastPrefixed(text.Of(text.T("Heap dump summary written to: ", text.Gold), text.T(file, text.Gray)))
			async.BroadcastPrefixed(text.T("You can read the heap dump summary file using the viewer web-app - "+p.ViewerURL, text.Gray))
			p.logActivity(async, "Heap dump summary", activitylog.DataFile, file)
		}
	})
	return nil
}

// heapDump writes the runtime's heap dump, compressed when asked.
// spark: HeapAnalysisModule.heapDump
func heapDump(p *Platform, _ plugin.CommandSender, r *Responder, args *Arguments) error {
	file, err := p.saveFile("heap", "heapdump")
	if err != nil {
		return err
	}
	// The dump holds every object, live or not; a collection first leaves
	// only the live ones, as a JVM's live-only dump does.
	runGC := args.Bool("run-gc-before") || !args.Bool("include-non-live")
	if args.Bool("run-gc-before") {
		r.BroadcastPrefixed(text.T("Running garbage collector..."))
	}
	compress := ""
	if c := args.Strings("compress"); len(c) > 0 {
		compress = strings.ToLower(c[0])
	}
	r.BroadcastPrefixed(text.T("Creating a new heap dump, please wait..."))
	async := r.Async()
	p.Server.Scheduler().RunAsync(func() {
		if runGC {
			runtime.GC()
		}
		if err := heap.Dump(file); err != nil {
			async.BroadcastPrefixed(text.T("An error occurred whilst creating a heap dump.", text.Red))
			p.Server.Logger().Error("An error occurred whilst creating a heap dump.", "error", err)
			return
		}
		async.BroadcastPrefixed(text.Of(text.T("Heap dump written to: ", text.Gold), text.T(file, text.Gray)))
		p.logActivity(async, "Heap dump", activitylog.DataFile, file)
		if compress == "gzip" {
			if err := heapDumpCompress(async, file); err != nil {
				p.Server.Logger().Error("An error occurred whilst compressing the heap dump.", "error", err)
			}
		}
	})
	return nil
}

// heapDumpCompress gzips a heap dump, reporting progress every 5 seconds.
// spark: HeapAnalysisModule.heapDumpCompress
func heapDumpCompress(r *Responder, file string) error {
	r.BroadcastPrefixed(text.T("Compressing heap dump, please wait..."))
	info, err := os.Stat(file)
	if err != nil {
		return err
	}
	size := info.Size()
	var lastReport atomic.Int64
	lastReport.Store(time.Now().UnixNano())
	compressed, err := heap.Gzip(file, func(read int64) {
		now := time.Now().UnixNano()
		if now-lastReport.Load() > int64(5*time.Second) {
			lastReport.Store(now)
			r.BroadcastPrefixed(text.Colored(text.Gray, text.T("Compressed "), text.T(text.Bytes(read), text.Gold), text.T(" / "),
				text.T(text.Bytes(size), text.Gold), text.T(" so far... ("), text.T(text.Percent(float64(read), float64(size)), text.Green), text.T(")")))
		}
	})
	if err != nil {
		return err
	}
	cinfo, err := os.Stat(compressed)
	if err != nil {
		return err
	}
	r.BroadcastPrefixed(text.Colored(text.Gray, text.T("Compression complete: "), text.T(text.Bytes(size), text.Gold), text.T(" --> "),
		text.T(text.Bytes(cinfo.Size()), text.Gold), text.T(" ("), text.T(text.Percent(float64(cinfo.Size()), float64(size)), text.Green), text.T(")")))
	r.BroadcastPrefixed(text.Of(text.T("Compressed heap dump written to: ", text.Gold), text.T(compressed, text.Gray)))
	return nil
}
