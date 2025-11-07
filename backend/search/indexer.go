package search

import (
	"bytes"
	"log/slog"
	"net/url"
	"rdf-store-backend/base"
	"rdf-store-backend/shacl"
	"rdf-store-backend/sparql"
	"time"

	"github.com/deiu/rdf2go"
)

func Init(forceRecreate bool) (err error) {
	if forceRecreate || !checkCollectionExists() {
		err = recreateCollection()
	}
	return
}

func Reindex() {
	slog.Info("reindexing...")
	start := time.Now()
	if err := Init(true); err != nil {
		slog.Error("reindexing failed.", "error", err)
		return
	}
	resourceIds, err := sparql.GetAllResourceIds()
	if err != nil {
		slog.Error("reindexing failed.", "error", err)
		return
	}
	resourceCount := 0
	for _, id := range resourceIds {
		data, metadata, err := sparql.LoadResource(id, true)
		if err != nil {
			slog.Error("failed loading resource", "id", id, "error", err)
		} else {
			graph, err := base.ParseGraph(bytes.NewReader(data))
			if err != nil {
				slog.Error(err.Error())
			} else {
				resourceID := rdf2go.NewResource(id)
				_, profile, err := sparql.FindResourceProfile(graph, &resourceID)
				if err != nil {
					slog.Error(err.Error())
				} else {
					if err = IndexResource(resourceID, profile, graph, metadata); err != nil {
						slog.Error("failed indexing resource", "id", id, "error", err)
					} else {
						resourceCount = resourceCount + 1
					}
				}
			}
		}
	}
	slog.Info("reindexing finished", "resources", resourceCount, "duration", time.Since(start))
}

func IndexResource(id rdf2go.Term, profile *shacl.NodeShape, graph *rdf2go.Graph, metadata *sparql.ResourceMetadata) error {
	slog.Debug("indexing", "resource", id.RawValue(), "creator", metadata.Creator, "profile", profile.Id.RawValue())

	var buf bytes.Buffer
	if err := graph.Serialize(&buf, "text/turtle"); err != nil {
		return err
	}

	// put profile and its parents into _shape index field (for inheritance)
	documentProfileIds := []string{profile.Id.RawValue()}
	for parentProfile := range profile.Parents {
		documentProfileIds = append(documentProfileIds, parentProfile.RawValue())
	}

	doc := document{
		"_rdf":          {buf.String()},
		"_shape":        {documentProfileIds},
		"_creator":      {metadata.Creator},
		"_lastModified": {metadata.LastModified},
		"id":            {url.QueryEscape(id.RawValue())},
	}
	appendPropertiesToDoc(id, doc, profile, graph)
	return updateDoc(doc)
}

func DeindexResource(id string) error {
	return deleteDoc(id)
}

func appendPropertiesToDoc(subject rdf2go.Term, doc document, profile *shacl.NodeShape, data *rdf2go.Graph) {
	for parent := range profile.Parents {
		if parentProfile, ok := sparql.Profiles[parent.RawValue()]; ok {
			appendPropertiesToDoc(subject, doc, parentProfile, data)
		}
	}
	for _, property := range profile.Properties {
		values := data.All(subject, property.Path, nil)
		if len(values) > 0 {
			array := make([]interface{}, 0)
			for _, value := range values {
				if len(property.NodeShapes) > 0 {
					for _, nodeProfileId := range property.NodeShapes {
						if nodeProfile, ok := sparql.Profiles[(*nodeProfileId).RawValue()]; ok {
							appendPropertiesToDoc(value.Object, doc, nodeProfile, data)
						}
					}
				} else if !property.Ignore {
					if _, ok := value.Object.(*rdf2go.Resource); ok {
						array = append(array, value.Object.String())
					} else {
						array = append(array, value.Object.RawValue())
					}
				}
			}

			doc[property.FieldName] = append(doc[property.FieldName], array)
		}
	}
}
