package main

import (
	"errors"
	"testing"
)

func TestRebuildAndReindexStopsBeforeReindexOnMigrationFailure(t *testing.T) {
	rebuildErr := errors.New("rebuild failed")
	reindexed := false
	err := rebuildAndReindex(
		func() error { return rebuildErr },
		func() error {
			reindexed = true
			return nil
		},
	)
	if !errors.Is(err, rebuildErr) {
		t.Fatalf("expected rebuild error, got %v", err)
	}
	if reindexed {
		t.Fatal("reindex must not run after a metadata migration failure")
	}
}

func TestRebuildAndReindexReturnsReindexFailure(t *testing.T) {
	reindexErr := errors.New("reindex failed")
	err := rebuildAndReindex(func() error { return nil }, func() error { return reindexErr })
	if !errors.Is(err, reindexErr) {
		t.Fatalf("expected reindex error, got %v", err)
	}
}
