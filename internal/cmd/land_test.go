package cmd

import (
	"strings"
	"testing"
)

func TestWithSiblingBlock_append(t *testing.T) {
	body := "Some PR description."
	got := withSiblingBlock(body, "my-feature", []string{"owner/api#1", "owner/payments#2"})
	if !strings.Contains(got, siblingBlockStart) {
		t.Errorf("missing start marker: %q", got)
	}
	if !strings.Contains(got, siblingBlockEnd) {
		t.Errorf("missing end marker: %q", got)
	}
	if !strings.Contains(got, "my-feature") {
		t.Errorf("missing workspace name: %q", got)
	}
	if !strings.Contains(got, "owner/api#1") {
		t.Errorf("missing sibling ref: %q", got)
	}
	if !strings.Contains(got, "Some PR description.") {
		t.Errorf("original body was lost: %q", got)
	}
}

func TestWithSiblingBlock_empty(t *testing.T) {
	got := withSiblingBlock("", "feat", []string{"owner/b#1"})
	if !strings.HasPrefix(got, siblingBlockStart) {
		t.Errorf("expected block at start of empty body, got: %q", got)
	}
}

func TestWithSiblingBlock_replace(t *testing.T) {
	initial := withSiblingBlock("Original body.", "feat", []string{"owner/a#1"})
	updated := withSiblingBlock(initial, "feat", []string{"owner/a#1", "owner/b#2"})

	// The original body must be present and the markers appear only once.
	if strings.Count(updated, siblingBlockStart) != 1 {
		t.Errorf("start marker appears %d times, want 1: %q", strings.Count(updated, siblingBlockStart), updated)
	}
	if !strings.Contains(updated, "owner/b#2") {
		t.Errorf("updated block missing new sibling: %q", updated)
	}
	if !strings.Contains(updated, "Original body.") {
		t.Errorf("original body was lost after replace: %q", updated)
	}
}

func TestWithSiblingBlock_idempotent(t *testing.T) {
	body := "A description."
	siblings := []string{"owner/x#5"}
	first := withSiblingBlock(body, "feat", siblings)
	second := withSiblingBlock(first, "feat", siblings)
	if first != second {
		t.Errorf("withSiblingBlock not idempotent:\nfirst:  %q\nsecond: %q", first, second)
	}
}
