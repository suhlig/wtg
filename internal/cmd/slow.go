package cmd

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/suhlig/wtg/internal/git"
	"github.com/suhlig/wtg/internal/remote"
)

// slowThreshold is how long a git command runs before it is reported as slow;
// slowInterval is how often the progress bar refreshes that report. Variables
// so tests can shrink them.
var (
	slowThreshold = 30 * time.Second
	slowInterval  = 5 * time.Second
	slowOut       = io.Writer(os.Stderr)
)

// A runner that can say which commands are running right now implements
// Active: git.SystemRunner for git, remote.SystemRunner for gh. Test doubles
// need not implement it, and then nothing is ever reported slow.
type (
	gitActive interface{ Active() []git.ActiveOp }
	ghActive  interface{ Active() []remote.ActiveOp }
)

// execProcs tracks the user commands `wtg exec` has running, so a slow one is
// named like a slow git call and Ctrl-C can list and stop it.
var execProcs = &execTracker{ops: map[int]execOp{}}

type execOp struct {
	runningOp
	stop func() // stops the command's process group; nil when it is not in one
}

type execTracker struct {
	mu      sync.Mutex
	ops     map[int]execOp
	nextID  int
	stopped bool // StopProcesses ran: commands registered from now on are stopped at once
}

// begin registers a command running for the repo called name. stop, if not nil,
// ends it on interrupt.
func (t *execTracker) begin(name string, stop func()) (end func()) {
	t.mu.Lock()
	id := t.nextID
	t.nextID++
	t.ops[id] = execOp{runningOp{name, time.Now()}, stop}
	stopped := t.stopped
	t.mu.Unlock()
	if stopped && stop != nil {
		stop() // wtg is shutting down; do not start what the loop reaches next
	}
	return func() {
		t.mu.Lock()
		delete(t.ops, id)
		t.mu.Unlock()
	}
}

func (t *execTracker) active() []runningOp {
	t.mu.Lock()
	ops := make([]runningOp, 0, len(t.ops))
	for _, op := range t.ops {
		ops = append(ops, op.runningOp)
	}
	t.mu.Unlock()
	sort.Slice(ops, func(i, j int) bool { return ops[i].since.Before(ops[j].since) })
	return ops
}

// StopProcesses interrupts the `wtg exec` commands still running. A terminal's
// Ctrl-C reaches them anyway, except under --timeout (own process group) or
// when the signal was sent to wtg alone. main calls it when the user interrupts.
func StopProcesses() {
	execProcs.mu.Lock()
	defer execProcs.mu.Unlock()
	execProcs.stopped = true
	for _, op := range execProcs.ops {
		if op.stop != nil {
			op.stop()
		}
	}
}

// ActiveProcesses describes the `wtg exec` commands still running, one per line
// with the repo and how long each has been going. Empty when none is. main
// prints it when the user interrupts.
func ActiveProcesses(now time.Time) string {
	var b strings.Builder
	for _, op := range execProcs.active() {
		fmt.Fprintf(&b, "  command in %s (%s)\n", op.label, now.Sub(op.since).Round(time.Second))
	}
	return b.String()
}

// runners watches several runners at once, for a command that uses both git
// and gh.
type runners []any

// tracksActive reports whether runner can list its running commands.
func tracksActive(runner any) bool {
	switch r := runner.(type) {
	case gitActive, ghActive, *execTracker:
		return true
	case runners:
		for _, x := range r {
			if tracksActive(x) {
				return true
			}
		}
	}
	return false
}

// runningOp is a command in flight: what to call it, and since when.
type runningOp struct {
	label string
	since time.Time
}

// runningOps returns the runner's commands, oldest first. label maps the
// directory a git command ran in to the name shown to the user; gh commands
// are labelled by ghLabel.
func runningOps(runner any, label func(path string) string) []runningOp {
	var ops []runningOp
	switch r := runner.(type) {
	case gitActive:
		for _, op := range r.Active() {
			ops = append(ops, runningOp{label(op.Repo), op.Since})
		}
	case ghActive:
		for _, op := range r.Active() {
			ops = append(ops, runningOp{ghLabel(op.Args), op.Since})
		}
	case *execTracker:
		ops = r.active()
	case runners:
		for _, x := range r {
			ops = append(ops, runningOps(x, label)...)
		}
		sort.Slice(ops, func(i, j int) bool { return ops[i].since.Before(ops[j].since) })
	}
	return ops
}

// ghLabel names a gh command compactly: its first two words and the repo it
// targets, e.g. "gh pr list org/api". The rest of the arguments (a PR body, for
// one) can be long and say nothing about where the time goes.
func ghLabel(args []string) string {
	words := args
	if len(words) > 2 {
		words = words[:2]
	}
	label := "gh " + strings.Join(words, " ")
	for i, a := range args {
		if a == "--repo" && i+1 < len(args) {
			return label + " " + args[i+1]
		}
	}
	if len(args) > 2 && !strings.HasPrefix(args[2], "-") {
		return label + " " + args[2] // gh repo view <owner/repo>
	}
	return label
}

// slowParts lists the commands that have run longer than slowThreshold, longest
// first, each as "<label> <duration>". It is empty when nothing is slow or the
// runner does not track its commands.
func slowParts(runner any, label func(path string) string) []string {
	now := time.Now()
	var parts []string
	for _, op := range runningOps(runner, label) {
		if age := now.Sub(op.since); age >= slowThreshold {
			parts = append(parts, fmt.Sprintf("%s %s", op.label, age.Round(time.Second)))
		}
	}
	return parts
}

// slowBarNote is the short form for the progress bar: the longest-running
// command, plus how many others are slow too. Empty when nothing is slow.
func slowBarNote(runner any, label func(path string) string) string {
	parts := slowParts(runner, label)
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return "slow: " + parts[0]
	default:
		return fmt.Sprintf("slow: %s +%d", parts[0], len(parts)-1)
	}
}

// watchSlow calls tick every interval until the returned stop function is
// called. stop waits for a tick in progress to finish, so none runs after it
// returns. It does nothing for a runner that does not track its commands.
func watchSlow(runner any, interval time.Duration, tick func()) (stop func()) {
	if !tracksActive(runner) {
		return func() {}
	}
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				tick()
			}
		}
	}()
	return func() {
		close(done)
		<-finished
	}
}

// noticeSlow prints the slow commands to slowOut every slowThreshold while
// a command without a progress bar runs, so a long wait names the repo it is
// waiting on. A nil runner is fine and reports nothing. The returned function
// stops it.
func noticeSlow(runner any, label func(path string) string) (stop func()) {
	return watchSlow(runner, slowThreshold, func() {
		if parts := slowParts(runner, label); len(parts) > 0 {
			_, _ = fmt.Fprintf(slowOut, "wtg: still waiting for: %s\n", strings.Join(parts, ", "))
		}
	})
}

// labelByPath returns a label function that names a directory by its entry in
// labels, falling back to the path itself.
func labelByPath(labels map[string]string) func(string) string {
	return func(p string) string {
		if l, ok := labels[p]; ok {
			return l
		}
		return p
	}
}
