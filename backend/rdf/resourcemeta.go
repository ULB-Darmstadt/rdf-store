package rdf

import (
	"bytes"
	"fmt"
	"text/template"
	"time"

	"github.com/knakk/rdf"
	"github.com/knakk/sparql"
)

type ResourceMetadata struct {
	Creator      string
	LastModified time.Time
}

// loadResourceMetadata reads resource metadata triples.
func loadResourceMetadata(id string) (metadata *ResourceMetadata, err error) {
	metadata = &ResourceMetadata{}
	bindings, err := queryDataset(ResourceDataset, fmt.Sprintf(`SELECT ?p ?o WHERE { <%s> ?p ?o }`, id))
	if err != nil {
		return
	}
	res, err := sparql.ParseJSON(bytes.NewReader(bindings))
	if err != nil {
		return nil, err
	}
	for _, row := range res.Solutions() {
		p, okP := row["p"].(rdf.Predicate)
		o, okO := row["o"].(rdf.Object)
		if !okP || !okO {
			return nil, fmt.Errorf("invalid binding: %v", row)
		}
		switch p.String() {
		case dcTermsCreator:
			metadata.Creator = o.String()
		case dcTermsModified:
			if date, err := time.Parse(time.RFC3339, o.String()); err == nil {
				metadata.LastModified = date
			}
		}
	}
	return
}

var metadataUpdateTemplate = template.Must(template.New("").Funcs(template.FuncMap{
	"FormatTime": func(t time.Time) string {
		return t.UTC().Format(time.RFC3339)
	},
}).Parse(`
	INSERT DATA {
		<{{.Id}}> <` + dcTermsModified + `> "{{FormatTime .Metadata.LastModified}}"^^<http://www.w3.org/2001/XMLSchema#dateTime>  .
		{{if gt (len (.Metadata.Creator)) 0}}
		<{{.Id}}> <` + dcTermsCreator + `> "{{.Metadata.Creator}}" .
		{{- end}}
	}
`))

// updateResourceMetadata writes updated creator and modified timestamp triples.
func updateResourceMetadata(id string, creator string) (metadata *ResourceMetadata, err error) {
	deleteResourceMetadata(id)
	metadata = &ResourceMetadata{
		Creator:      creator,
		LastModified: time.Now().UTC(),
	}
	tmplInput := map[string]any{
		"Id":       id,
		"Metadata": metadata,
	}
	var buf bytes.Buffer
	if err = metadataUpdateTemplate.Execute(&buf, tmplInput); err != nil {
		return
	}
	err = updateDataset(ResourceDataset, buf.String())
	return
}

// deleteResourceMetadata removes all metadata triples for a resource.
func deleteResourceMetadata(id string) error {
	return updateDataset(ResourceDataset, fmt.Sprintf("DELETE WHERE { <%s> ?p ?o }", id))
}
