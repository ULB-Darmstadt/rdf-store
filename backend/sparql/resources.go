package sparql

import (
	"bytes"
	"fmt"
	"log/slog"
	"text/template"
	"time"

	"github.com/antchfx/xmlquery"
)

var prefixDcTerms = "http://purl.org/dc/terms/%s"
var dcTermsCreator = fmt.Sprintf(prefixDcTerms, "creator")
var dcTermsModified = fmt.Sprintf(prefixDcTerms, "modified")

func LoadResource(id string, union bool) (resource []byte, metadata *ResourceMetadata, err error) {
	if union {
		resource, err = queryDataset(ResourceDataset, fmt.Sprintf(`CONSTRUCT { ?s ?p ?o } WHERE { GRAPH <%s> { ?s (<>|!<>)* ?s . GRAPH ?g { ?s ?p ?o } } }`, id))
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
	data, err := queryDataset(ResourceDataset, fmt.Sprintf(`SELECT ?p ?o WHERE { <%s> ?p ?o }`, id))
	if err != nil {
		return
	}
	doc, err := xmlquery.Parse(bytes.NewReader(data))
	if node := xmlquery.FindOne(doc, "/sparql/results/result[binding[@name='p']/uri[text()='"+dcTermsCreator+"']]/binding[@name='o']/literal"); node != nil {
		metadata.Creator = node.InnerText()
	}
	if node := xmlquery.FindOne(doc, "/sparql/results/result[binding[@name='p']/uri[text()='"+dcTermsModified+"']]/binding[@name='o']/literal"); node != nil {
		if date, err := time.Parse(time.RFC3339, node.InnerText()); err == nil {
			metadata.LastModified = date
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
	tmplInput := map[string]interface{}{
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
