package sparql

import (
	"bytes"
	"fmt"
	"log/slog"
	"text/template"
	"time"

	"github.com/knakk/rdf"
	"github.com/knakk/sparql"
)

var prefixDcTerms = "http://purl.org/dc/terms/%s"
var dcTermsCreator = fmt.Sprintf(prefixDcTerms, "creator")
var dcTermsModified = fmt.Sprintf(prefixDcTerms, "modified")

func GetResource(id string, union bool) (resource []byte, metadata *ResourceMetadata, err error) {
	if union {
		exists, err2 := checkGraphExists(ResourceDataset, id)
		if err2 != nil {
			return nil, nil, err2
		}
		if !exists {
			err = fmt.Errorf("graph %s not found in dataset %s", id, ResourceDataset)
			return nil, nil, err
		}
		var bindings []byte
		if bindings, err = queryDataset(ResourceDataset, fmt.Sprintf(`SELECT ?s ?p ?o ?g WHERE { GRAPH <%s> { ?s (<>|!<>)* ?s . GRAPH ?g { ?s ?p ?o } } }`, id)); err != nil {
			return
		}
		resource, err = sparqlResultToNQuads(bindings)
	} else {
		resource, err = loadGraph(ResourceDataset, id)
	}
	if err != nil {
		return
	}
	metadata, err = loadResourceMetadata(id)
	return
}

func CreateResource(id string, resource []byte, creator string) (*ResourceMetadata, error) {
	if err := createGraph(ResourceDataset, id, resource); err != nil {
		return nil, err
	}
	return updateResourceMetadata(id, creator)
}

func UpdateResource(id string, resource []byte, creator string) (*ResourceMetadata, error) {
	if err := validateCreator(id, creator); err != nil {
		return nil, err
	}
	if err := uploadGraph(ResourceDataset, id, resource, nil); err != nil {
		return nil, err
	}
	return updateResourceMetadata(id, creator)
}

func DeleteResource(id string, creator string) error {
	if err := validateCreator(id, creator); err != nil {
		return err
	}
	if err := deleteGraph(ResourceDataset, id); err != nil {
		return err
	}
	return deleteResourceMetadata(id)
}

func GetAllResourceIds() ([]string, error) {
	return getAllGraphIds(ResourceDataset)
}

func GetClassInstances(class string) ([]byte, error) {
	// prevent SPARQL injection
	if !isValidIRI(class) {
		return nil, fmt.Errorf("invalid class IRI: %v", class)
	}
	bindings, err := queryDataset(ResourceDataset, fmt.Sprintf(`SELECT ?s ?p ?o ?g WHERE  { GRAPH ?g { ?c a <%s> . ?c (<>|!<>)* ?s . ?s ?p ?o }}`, class))
	if err != nil {
		return nil, err
	}
	return sparqlResultToNQuads(bindings)
}

func validateCreator(id string, user string) error {
	if user == "" {
		return nil
	}
	metadata, err := loadResourceMetadata(id)
	if err != nil {
		return err
	}
	if metadata.Creator == user {
		return nil
	}
	slog.Warn("resource creator mismatch", "creator", metadata.Creator, "requesting user", user)
	return fmt.Errorf("creator mismatch")
}

type ResourceMetadata struct {
	Creator      string
	LastModified time.Time
}

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

func deleteResourceMetadata(id string) error {
	return updateDataset(ResourceDataset, fmt.Sprintf("DELETE WHERE { <%s> ?p ?o }", id))
}
