package cmd

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/suhlig/wtg/internal/git"
	"github.com/suhlig/wtg/internal/remote"
)

// activeRunner is a testRunner that also reports commands as running, the way
// git.SystemRunner does.
type activeRunner struct {
	*testRunner
	mu  sync.Mutex
	ops []git.ActiveOp
}

func (a *activeRunner) Active() []git.ActiveOp {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]git.ActiveOp(nil), a.ops...)
}

func (a *activeRunner) set(ops ...git.ActiveOp) {
	a.mu.Lock()
	a.ops = ops
	a.mu.Unlock()
}

func fastSlowTimers(t *testing.T) {
	t.Helper()
	origT, origI := slowThreshold, slowInterval
	slowThreshold, slowInterval = time.Second, 10*time.Millisecond
	t.Cleanup(func() { slowThreshold, slowInterval = origT, origI })
}

func TestSlowBarNote(t *testing.T) {
	fastSlowTimers(t)
	label := func(p string) string { return strings.TrimPrefix(p, "/r/") }
	old := time.Now().Add(-65 * time.Second)

	r := &activeRunner{testRunner: &testRunner{}}
	if got := slowBarNote(r, label); got != "" {
		t.Errorf("nothing running: got %q", got)
	}

	r.set(git.ActiveOp{Repo: "/r/fresh", Since: time.Now()})
	if got := slowBarNote(r, label); got != "" {
		t.Errorf("only a fresh command: got %q", got)
	}

	r.set(git.ActiveOp{Repo: "/r/api", Since: old}, git.ActiveOp{Repo: "/r/svc", Since: time.Now().Add(-3 * time.Second)})
	if got := slowBarNote(r, label); got != "slow: api 1m5s +1" {
		t.Errorf("two slow: got %q", got)
	}

	if got := slowBarNote(&testRunner{}, label); got != "" {
		t.Errorf("runner that does not track commands: got %q", got)
	}
}

// A sync that is stuck on one repo names it in the progress bar, without waiting
// for a result to redraw.
func TestRunSync_ProgressNamesSlowRepo(t *testing.T) {
	fastSlowTimers(t)
	root := t.TempDir()
	makeRepo(t, root, "api")

	gate := make(chan struct{})
	r := &activeRunner{testRunner: &testRunner{
		defaultBranchFn: func(string) (string, error) { return "main", nil },
		statusFn:        func(string) (git.RepoStatus, error) { return git.RepoStatus{Branch: "main"}, nil },
		fetchFn:         func(string) error { <-gate; return nil },
	}}
	r.set(git.ActiveOp{Repo: root + "/api", Args: []string{"fetch", "origin"}, Since: time.Now().Add(-2 * time.Second)})

	var out syncBuf
	done := make(chan error, 1)
	go func() { done <- RunSync(discoverCfg(root, 1), r, nil, true, &out) }()

	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(out.String(), "slow: api") && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := out.String(); !strings.Contains(got, "(1 fetching; slow: api") {
		t.Errorf("progress should name the slow repo, got %q", got)
	}
	close(gate)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// Without --progress, a long wait prints a line naming the repo.
func TestRunFetch_NamesSlowRepo(t *testing.T) {
	fastSlowTimers(t)
	root := t.TempDir()
	makeRepo(t, root, "api")

	var notices syncBuf
	orig := slowOut
	slowOut = &notices
	t.Cleanup(func() { slowOut = orig })

	gate := make(chan struct{})
	r := &activeRunner{testRunner: &testRunner{fetchFn: func(string) error { <-gate; return nil }}}
	r.set(git.ActiveOp{Repo: root + "/api", Since: time.Now().Add(-2 * time.Second)})

	var out bytes.Buffer
	done := make(chan error, 1)
	go func() { done <- RunFetch(discoverCfg(root, 1), r, nil, &out) }()

	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(notices.String(), "still waiting for: api") && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := notices.String(); !strings.Contains(got, "wtg: still waiting for: api") {
		t.Errorf("expected a notice naming the repo, got %q", got)
	}
	close(gate)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// syncBuf is a bytes.Buffer safe for use from the test and the command at once.
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuf) String() string              { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

// ghActiveRunner stands in for remote.SystemRunner's list of running commands.
type ghActiveRunner struct{ ops []remote.ActiveOp }

func (g ghActiveRunner) Active() []remote.ActiveOp { return g.ops }

func TestGHLabel(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"pr", "list", "--repo", "org/api", "--head", "feat"}, "gh pr list org/api"},
		{[]string{"repo", "view", "org/api", "--json", "isArchived"}, "gh repo view org/api"},
		{[]string{"auth", "status", "--hostname", "github.com"}, "gh auth status"},
		{[]string{"pr", "edit", "7", "--repo", "org/api", "--body", "a very long body"}, "gh pr edit org/api"},
	} {
		if got := ghLabel(tc.args); got != tc.want {
			t.Errorf("ghLabel(%v) = %q, want %q", tc.args, got, tc.want)
		}
	}
}

func TestSlowParts_GitAndGHTogether(t *testing.T) {
	fastSlowTimers(t)
	g := &activeRunner{testRunner: &testRunner{}}
	g.set(git.ActiveOp{Repo: "/r/api", Since: time.Now().Add(-90 * time.Second)})
	gh := ghActiveRunner{[]remote.ActiveOp{{Args: []string{"pr", "list", "--repo", "org/api"}, Since: time.Now().Add(-70 * time.Second)}}}

	got := slowParts(runners{g, gh}, func(p string) string { return strings.TrimPrefix(p, "/r/") })
	if len(got) != 2 || !strings.HasPrefix(got[0], "api 1m30s") || !strings.HasPrefix(got[1], "gh pr list org/api 1m10s") {
		t.Errorf("slowParts = %q, want git then gh, oldest first", got)
	}
}

func TestWatchSlow_NilAndUntrackedRunnersAreFine(t *testing.T) {
	noticeSlow(nil, nil)()
	noticeSlow(&testRunner{}, nil)()
	noticeSlow(runners{&testRunner{}, nil}, nil)()
}

func TestActiveProcesses(t *testing.T) {
	end := execProcs.begin("api", nil)
	defer end()
	got := ActiveProcesses(time.Now().Add(65 * time.Second))
	if !strings.Contains(got, "command in api (1m5s)") {
		t.Errorf("ActiveProcesses = %q", got)
	}
	end()
	if got := ActiveProcesses(time.Now()); got != "" {
		t.Errorf("after the command ended: %q", got)
	}
}

// After StopProcesses, a command the loop reaches next is stopped before it runs.
func TestStopProcesses_StopsLaterCommandsToo(t *testing.T) {
	execProcs.mu.Lock()
	execProcs.stopped = false
	execProcs.mu.Unlock()
	t.Cleanup(func() {
		execProcs.mu.Lock()
		execProcs.stopped = false
		execProcs.mu.Unlock()
	})

	var first, second bool
	end1 := execProcs.begin("api", func() { first = true })
	defer end1()
	StopProcesses()
	if !first {
		t.Error("a running command should be stopped")
	}
	end2 := execProcs.begin("svc", func() { second = true })
	defer end2()
	if !second {
		t.Error("a command registered after StopProcesses should be stopped at once")
	}
}
