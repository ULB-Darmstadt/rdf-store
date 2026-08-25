package api

import (
	"errors"
	"reflect"
	"testing"

	"rdf-store-backend/rdf"
)

func TestRollbackLinkedResourceUpdateAttemptsEveryRecoveryStep(t *testing.T) {
	updateErr := errors.New("dependent validation failed")
	metadataErr := errors.New("metadata restore failed")
	indexErr := errors.New("index restore failed")
	target := &rdf.ResourceMetadata{Id: nil}
	first := &rdf.ResourceMetadata{}
	second := &rdf.ResourceMetadata{}
	var restoredMetadata []*rdf.ResourceMetadata
	var indexed []string

	err := rollbackLinkedResourceUpdateWith(
		[]byte("old resource"), target,
		map[string]*rdf.ResourceMetadata{"first": first, "second": second},
		[]string{"target", "first", "second"}, updateErr,
		func(data []byte, metadata *rdf.ResourceMetadata) error {
			if string(data) != "old resource" || metadata != target {
				t.Fatalf("unexpected resource snapshot")
			}
			return nil
		},
		func(metadata *rdf.ResourceMetadata) error {
			restoredMetadata = append(restoredMetadata, metadata)
			if metadata == first {
				return metadataErr
			}
			return nil
		},
		func(ids []string) error {
			indexed = append(indexed, ids...)
			return indexErr
		},
	)
	if !errors.Is(err, updateErr) || !errors.Is(err, metadataErr) || !errors.Is(err, indexErr) {
		t.Fatalf("expected original and recovery errors, got %v", err)
	}
	if len(restoredMetadata) != 2 {
		t.Fatalf("expected every metadata snapshot to be restored, got %d", len(restoredMetadata))
	}
	if !reflect.DeepEqual(indexed, []string{"target", "first", "second"}) {
		t.Fatalf("expected every affected resource to be reindexed, got %#v", indexed)
	}
}

func TestRollbackLinkedResourceUpdateReturnsOriginalErrorAfterSuccessfulRecovery(t *testing.T) {
	updateErr := errors.New("update failed")
	err := rollbackLinkedResourceUpdateWith(nil, nil, nil, []string{"target"}, updateErr,
		func([]byte, *rdf.ResourceMetadata) error { return nil },
		func(*rdf.ResourceMetadata) error { return nil },
		func([]string) error { return nil },
	)
	if err != updateErr {
		t.Fatalf("expected original error after successful recovery, got %v", err)
	}
}
