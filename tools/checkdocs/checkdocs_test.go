package main

import (
	"testing"
)

func TestCheckDocs(t *testing.T) {
	if err := checkTranscripts("../../site/data/transcripts"); err != nil {
		t.Fatalf("checkTranscripts failed: %v", err)
	}

	if err := checkContent("../../site/content", "../../site/data/transcripts"); err != nil {
		t.Fatalf("checkContent failed: %v", err)
	}
}
