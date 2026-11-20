package sampler

import (
	"slices"

	"github.com/golem-mc/spark/internal/sparkproto"
)

// maxStackDepth is how many frames from the root are kept.
// spark: AbstractNode.MAX_STACK_DEPTH
const maxStackDepth = 300

// Frame is one stack frame, as spark describes a Java one.
// spark: StackTraceNode.JavaDescription
type Frame struct {
	Class, Method string
	// Line is the frame's line, ParentLine the line its caller is at; -1
	// for none.
	Line, ParentLine int32
}

// node is a call tree node: time per window and the calls made from it.
// spark: AbstractNode, StackTraceNode, ThreadNode
type node struct {
	frame    Frame
	times    map[int]int64 // window -> microseconds
	children map[Frame]*node
	order    []Frame // children in the order they were first seen
}

func newNode(f Frame) *node {
	return &node{frame: f, times: map[int]int64{}, children: map[Frame]*node{}}
}

func (n *node) child(f Frame) *node {
	c, ok := n.children[f]
	if !ok {
		c = newNode(f)
		n.children[f] = c
		n.order = append(n.order, f)
	}
	return c
}

// log adds time to the path from the root through stack, which runs from
// the outermost call to the innermost. spark: ThreadNode.log
func (n *node) log(stack []Frame, micros int64, window int) {
	if len(stack) == 0 {
		return
	}
	n.times[window] += micros
	cur := n
	for _, f := range stack[:min(len(stack), maxStackDepth)] {
		cur = cur.child(f)
		cur.times[window] += micros
	}
}

// prune drops the windows keep rejects, and the children left with none;
// it reports whether n is left with none. spark: ThreadNode.removeTimeWindowsRecursively
func (n *node) prune(drop func(window int) bool) bool {
	for w := range n.times {
		if drop(w) {
			delete(n.times, w)
		}
	}
	n.order = slices.DeleteFunc(n.order, func(f Frame) bool {
		if n.children[f].prune(drop) {
			delete(n.children, f)
			return true
		}
		return false
	})
	return len(n.times) == 0
}

// merge adds other's times and children into n. spark: AbstractNode.merge
func (n *node) merge(other *node) {
	for w, t := range other.times {
		n.times[w] += t
	}
	for _, f := range other.order {
		n.child(f).merge(other.children[f])
	}
}

func (n *node) copy() *node {
	c := newNode(n.frame)
	c.merge(n)
	return c
}

// windows adds the windows n has time in to set.
func (n *node) windows(set map[int]bool) {
	for w := range n.times {
		set[w] = true
	}
}

// timeEncoder turns a node's times into the proto's array, one value per
// window in key order, in milliseconds. spark: ProtoTimeEncoder
type timeEncoder struct {
	keys  []int
	index map[int]int
}

func newTimeEncoder(windows map[int]bool) *timeEncoder {
	e := &timeEncoder{index: map[int]int{}}
	for w := range windows {
		e.keys = append(e.keys, w)
	}
	slices.Sort(e.keys)
	for i, w := range e.keys {
		e.index[w] = i
	}
	return e
}

func (e *timeEncoder) encode(times map[int]int64) []float64 {
	out := make([]float64, len(e.keys))
	for w, t := range times {
		out[e.index[w]] = float64(t) / 1000
	}
	return out
}

// mergeChildren merges the children that are the same method, unless
// separateParentCalls keeps calls from different caller lines apart.
// spark: JavaNodeExporter.exportChildren, MergeStrategy
func mergeChildren(n *node, separateParentCalls bool) []*node {
	var out []*node
	copied := map[int]bool{}
outer:
	for _, f := range n.order {
		c := n.children[f]
		for i, o := range out {
			if o.frame.Class == f.Class && o.frame.Method == f.Method && (!separateParentCalls || o.frame.ParentLine == f.ParentLine) {
				if !copied[i] {
					out[i] = o.copy()
					copied[i] = true
				}
				out[i].merge(c)
				continue outer
			}
		}
		out = append(out, c)
	}
	return out
}

// exportThread flattens a thread's tree into spark's array of nodes, each
// listing its children by index, in post order.
// spark: AbstractNodeExporter.export
func exportThread(name string, root *node, enc *timeEncoder, separateParentCalls bool) *sparkproto.ThreadNode {
	t := &sparkproto.ThreadNode{Name: name, Times: enc.encode(root.times)}
	var visit func(n *node) int32
	visit = func(n *node) int32 {
		var refs []int32
		for _, c := range mergeChildren(n, separateParentCalls) {
			refs = append(refs, visit(c))
		}
		p := &sparkproto.StackTraceNode{ClassName: n.frame.Class, MethodName: n.frame.Method, Times: enc.encode(n.times), ChildrenRefs: refs}
		if n.frame.Line >= 0 {
			p.LineNumber = n.frame.Line
		}
		if separateParentCalls && n.frame.ParentLine >= 0 {
			p.ParentLineNumber = n.frame.ParentLine
		}
		t.Children = append(t.Children, p)
		return int32(len(t.Children) - 1)
	}
	for _, c := range mergeChildren(root, separateParentCalls) {
		t.ChildrenRefs = append(t.ChildrenRefs, visit(c))
	}
	return t
}
