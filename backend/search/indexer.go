package search

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"rdf-store-backend/base"
	"rdf-store-backend/rdf"
	"reflect"
	"time"

	"github.com/deiu/rdf2go"
)

// Init prepares the Solr collection and schema for indexing.
// It returns an error if Solr cannot be reached or initialized.
func Init(forceRecreate bool) error {
	if forceRecreate {
		return recreateCollection()
	}
	const maxAttempts = 30
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		exists, err := checkCollectionExists(context.Background())
		if err != nil {
			slog.Warn("solr not ready yet", "attempt", attempt, "max_attempts", maxAttempts, "error", err)
		} else if exists {
			return nil
		} else {
			return recreateCollection()
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("solr not ready after %d attempts", maxAttempts)
}

// Reindex rebuilds the Solr index from all known resources.
func Reindex() {
	slog.Info("reindexing...")
	start := time.Now()
	if err := Init(true); err != nil {
		slog.Error("reindexing failed.", "error", err)
		return
	}
	resourceIds, err := rdf.GetAllResourceIds()
	if err != nil {
		slog.Error("reindexing failed.", "error", err)
		return
	}
	resourceCount := 0
	for _, id := range resourceIds {
		data, metadata, err := rdf.GetResource(id, false)
		if err != nil {
			slog.Error("failed loading resource", "id", id, "error", err)
		} else {
			graph, err := base.ParseGraph(bytes.NewReader(data))
			if err != nil {
				slog.Error(err.Error())
			} else {
				if err = IndexResource(graph, metadata); err != nil {
					slog.Error("failed indexing resource", "id", id, "error", err)
				} else {
					resourceCount = resourceCount + 1
				}
			}
		}
	}
	slog.Info("reindexing finished", "resources", resourceCount, "duration", time.Since(start))
}

// IndexResource builds and submits search documents for a resource.
// It returns an error when indexing or deindexing fails.
func IndexResource(resource *rdf2go.Graph, metadata *rdf.ResourceMetadata) error {
	if err := DeindexResource(metadata.Id.RawValue()); err != nil {
		return err
	}

	rootProfileId, ok := metadata.Conformance[metadata.Id.RawValue()]
	if !ok {
		slog.Warn("not indexing because root profile not found", "resource", metadata.Id.RawValue(), "creator", metadata.Creator)
		return nil
	}
	slog.Debug("indexing", "resource", metadata.Id.RawValue(), "creator", metadata.Creator)

	mainDocument := document{
		"id":           metadata.Id.RawValue(),
		"creator":      metadata.Creator,
		"lastModified": metadata.LastModified,
		"label":        findLabels(metadata.Id, resource),
		"docType":      "main",
	}
	containerDocument := document{
		"id":           "container_" + metadata.Id.RawValue(),
		"creator":      metadata.Creator,
		"lastModified": metadata.LastModified,
		"docType":      "container",
		"_children_":   []*document{&mainDocument},
	}
	buildDoc(metadata.Id, rootProfileId, rootProfileId, resource, &mainDocument)
	for resourceId, profileId := range metadata.Conformance {
		if resourceId != metadata.Id.RawValue() {
			resourceIdTerm := rdf2go.NewResource(resourceId)
			childDocument := document{
				"id":         resourceId,
				"label":      findLabels(resourceIdTerm, resource),
				"ref_shapes": []string{rootProfileId},
			}
			buildDoc(resourceIdTerm, profileId, profileId, resource, &childDocument)
			appendMultiValue("_children_", &childDocument, &mainDocument)
			appendMultiValue("ref_shapes", profileId, &mainDocument)
		}
	}
	return updateDoc(&containerDocument)
}

// DeindexResource removes documents associated with a resource ID.
// It returns an error if the deletion request fails.
func DeindexResource(id string) error {
	return deleteDocs(id)
}

// buildDoc recursively constructs Solr documents from RDF graph data.
func buildDoc(subject rdf2go.Term, profileId string, rootProfileId string, resource *rdf2go.Graph, current *document) {
	slog.Debug("build doc", "subject", subject.RawValue(), "profile", profileId, "current", (*current)["id"])
	profile, ok := rdf.Profiles[profileId]
	if !ok {
		slog.Warn("profile not found", "id", profileId)
		return
	}
	// append shape conformance
	appendMultiValue("shape", profileId, current)

	// append property values to document
	for pathId, properties := range profile.Properties {
		path := rdf2go.NewResource(pathId)
		for _, property := range properties {
			ft := fieldType(property)
			for _, value := range resource.All(subject, path, nil) {
				raw := value.Object.RawValue()
				if ft == "t" {
					appendMultiValue("_text_", raw, current)
				} else {
					if ft == "dts" && len(raw) == 10 {
						raw = raw + "T00:00:00Z"
					}
					field := base.CleanStringForSolr(rootProfileId) + "." + base.CleanStringForSolr(property.Id.RawValue()) + "_" + ft
					appendMultiValue(field, raw, current)
				}
				resource.Remove(value)
			}
		}
	}

	for parentId := range profile.Parents {
		// append properties of all parent shapes to same document
		buildDoc(subject, parentId, rootProfileId, resource, current)
	}
}

// appendMultiValue appends values to a multi-value Solr field.
func appendMultiValue(field string, value any, doc *document) {
	if value == nil {
		return
	}
	existing, ok := (*doc)[field].([]any)
	if !ok {
		existing = make([]any, 0)
	}
	valueRef := reflect.ValueOf(value)
	if valueRef.Kind() == reflect.Slice {
		for i := 0; i < valueRef.Len(); i++ {
			existing = append(existing, valueRef.Index(i).Interface())
		}
	} else {
		existing = append(existing, value)
	}
	(*doc)[field] = existing
}

// findLabels collects literal labels for a subject in the graph.
// It returns the collected label strings.
func findLabels(subject rdf2go.Term, data *rdf2go.Graph) (labels []string) {
	for _, triple := range data.All(subject, nil, nil) {
		if _, ok := rdf.LabelPredicates[triple.Predicate.RawValue()]; ok {
			labels = append(labels, triple.Object.RawValue())
		}
	}
	return labels
}
