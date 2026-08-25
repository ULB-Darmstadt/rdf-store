package rdf

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/deiu/rdf2go"
)

func TestMetadataTemplateKeepsOwnedAndQueryConformanceSeparate(t *testing.T) {
	metadata := &ResourceMetadata{
		Id: rdf2go.NewResource("https://example.org/root"),
		Conformance: map[string][]string{
			"https://example.org/root": {"https://example.org/root-shape"},
		},
		QueryConformance: map[string][]string{
			"https://example.org/root":   {"https://example.org/root-shape"},
			"https://example.org/linked": {"https://example.org/linked-shape"},
		},
	}
	var output bytes.Buffer
	if err := metadataUpdateTemplate.Execute(&output, metadata); err != nil {
		t.Fatal(err)
	}
	rdf := output.String()
	owned := `<https://example.org/root> <http://purl.org/dc/terms/conformsTo> <https://example.org/root-shape>`
	linkedQuery := `<https://example.org/linked> <urn:rdf-store:queryConformsTo> <https://example.org/linked-shape>`
	if !strings.Contains(rdf, owned) || !strings.Contains(rdf, linkedQuery) {
		t.Fatalf("metadata does not contain separated conformance triples: %s", rdf)
	}
	if strings.Contains(rdf, `<https://example.org/linked> <http://purl.org/dc/terms/conformsTo>`) {
		t.Fatalf("linked query conformance leaked into owned conformance: %s", rdf)
	}
}

func TestRebuildResourceConformanceSetAttemptsAllResources(t *testing.T) {
	firstErr := errors.New("first failed")
	thirdErr := errors.New("third failed")
	var rebuilt []string
	err := rebuildResourceConformanceSet([]string{"first", "second", "third"}, func(id string) error {
		rebuilt = append(rebuilt, id)
		switch id {
		case "first":
			return firstErr
		case "third":
			return thirdErr
		default:
			return nil
		}
	})
	if !reflect.DeepEqual(rebuilt, []string{"first", "second", "third"}) {
		t.Fatalf("expected every resource to be attempted, got %#v", rebuilt)
	}
	if !errors.Is(err, firstErr) || !errors.Is(err, thirdErr) {
		t.Fatalf("expected joined rebuild errors, got %v", err)
	}
}

func TestCloneConformanceDoesNotShareShapeSlices(t *testing.T) {
	original := map[string][]string{"https://example.org/id": {"https://example.org/shape"}}
	clone := cloneConformance(original)
	clone["https://example.org/id"][0] = "https://example.org/changed"
	if original["https://example.org/id"][0] != "https://example.org/shape" {
		t.Fatalf("clone mutated original conformance: %#v", original)
	}
}
