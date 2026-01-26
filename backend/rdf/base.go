package rdf

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path"
	"rdf-store-backend/base"
	"strings"

	"github.com/deiu/rdf2go"
	"github.com/knakk/rdf"
	"github.com/knakk/sparql"
)

var FusekiEndpoint = base.EnvVar("FUSEKI_ENDPOINT", "http://localhost:3030")
var AuthHeader = fmt.Sprintf("Basic %s", base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", base.EnvVar("FUSEKI_USER", "admin"), base.EnvVar("FUSEKI_PASSWORD", "secret")))))
var ResourceDataset = base.EnvVar("FUSEKI_RESOURCE_DATASET", "resource")
var resourceMetaDataset = base.EnvVar("FUSEKI_RESOURCE_META_DATASET", "resourcemeta")
var profileDataset = base.EnvVar("FUSEKI_PROFILE_DATASET", "profile")
var labelDataset = base.EnvVar("FUSEKI_LABEL_DATASET", "label")

// init prepares datasets and imports local resources and labels.
func init() {
	if err := initDatasets(); err != nil {
		log.Fatal("failed initializing datasets", err)
	}
	if err := importLocalResources(); err != nil {
		slog.Error("failed importing local resources", "error", err)
	}
	if err := importLabelsFromStandardTaxonomies(); err != nil {
		slog.Error("failed importing standard taxonomies", "error", err)
	}
}

// initDatasets ensures required Fuseki datasets exist.
// It returns an error when dataset creation or checks fail.
func initDatasets() error {
	for _, dataset := range []string{ResourceDataset, resourceMetaDataset, profileDataset, labelDataset} {
		// check if dataset exists
		req, err := http.NewRequest("GET", fmt.Sprintf("%s/$/stats/%s", FusekiEndpoint, dataset), nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", AuthHeader)
		status, body, err := doRequest(req)
		if err != nil {
			return err
		}
		if status != http.StatusOK {
			// dataset does not exist, so create it
			req, err = http.NewRequest(http.MethodPost, fmt.Sprintf("%s/$/datasets?dbName=%s&dbType=TDB2", FusekiEndpoint, dataset), nil)
			if err != nil {
				return err
			}
			req.Header.Set("Authorization", AuthHeader)
			// req.Header.Set("Content-Type", "text/turtle")
			status, body, err = doRequest(req)
			if err != nil {
				return err
			}
			if !statusIsOK(status) {
				return newHTTPError(fmt.Sprintf("failed creating dataset %s", dataset), status, body)
			}
		}
	}
	return nil
}

// importLocalResources loads local RDF graphs into the resource dataset.
// It returns an error if any local graph cannot be read or uploaded.
func importLocalResources() error {
	baseDir := path.Join("local", "datagraph")
	if files, err := os.ReadDir(baseDir); err == nil {
		for _, file := range files {
			if !file.IsDir() && strings.HasSuffix(file.Name(), ".ttl") {
				slog.Info("importing resource graph", "file", file.Name())
				data, err := os.ReadFile(path.Join(baseDir, file.Name()))
				if err != nil {
					return err
				}
				if err = uploadGraph(ResourceDataset, file.Name(), data, nil); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// createGraph creates a new named graph in the target dataset.
// It returns an error if the graph already exists or upload fails.
func createGraph(dataset string, id string, data []byte) error {
	exists, err := checkGraphExists(dataset, id)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("graph %s already exists in dataset %s", id, dataset)
	}
	return uploadGraph(dataset, id, data, nil)
}

// loadGraph fetches a graph's triples from a dataset.
// It returns the serialized graph bytes and any error encountered.
func loadGraph(dataset string, id string) (data []byte, err error) {
	exists, err := checkGraphExists(dataset, id)
	if err != nil {
		return nil, err
	}
	if !exists {
		err = fmt.Errorf("graph %s not found in dataset %s", id, dataset)
		return nil, err
	}
	return queryDataset(dataset, fmt.Sprintf(`CONSTRUCT { ?s ?p ?o } WHERE { GRAPH <%s> { ?s ?p ?o } }`, id))
}

// uploadGraph replaces a named graph and optionally extracts labels.
// It returns an error if the upload or label extraction fails.
func uploadGraph(dataset string, id string, data []byte, graph *rdf2go.Graph) (err error) {
	if err = deleteGraph(dataset, id); err != nil {
		return
	}
	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	mimeHeader := make(textproto.MIMEHeader)
	mimeHeader.Set("Content-Disposition", `form-data; name="file"; filename="turtle.ttl"`)
	mimeHeader.Set("Content-Type", "text/turtle")
	part, err := writer.CreatePart(mimeHeader)
	if err != nil {
		return err
	}
	part.Write(data)
	writer.Close()

	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/%s/data?graph=%s", FusekiEndpoint, dataset, id), body)
	if err != nil {
		return err
	}
	req.Header.Add("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", AuthHeader)

	status, responseBody, err := doRequest(req)
	if err != nil {
		return err
	}
	if !statusIsOK(status) {
		return newHTTPError(fmt.Sprintf("failed uploading graph %s in dataset %s", id, dataset), status, responseBody)
	}
	// extract labels from profiles and resources
	if dataset == profileDataset || dataset == ResourceDataset {
		if graph == nil {
			graph, err = base.ParseGraph(bytes.NewReader(data))
			if err != nil {
				slog.Error("failed parsing graph for label extraction.", "id", id, "error", err)
			}
		}
		if err == nil {
			if err := ExtractLabels(id, graph, dataset == profileDataset); err != nil {
				slog.Error("failed extracting labels.", "id", id, "error", err)
			}
		}
	}
	return
}

// deleteGraph removes a named graph, associated labels and resource metadata.
// It returns an error if the deletion fails.
func deleteGraph(dataset string, id string) (err error) {
	err = updateDataset(dataset, fmt.Sprintf(`DROP GRAPH <%s>`, id))
	if err != nil {
		return
	}
	// delete labels that were sourced from the deleted graph
	if dataset == profileDataset || dataset == ResourceDataset {
		if err = deleteGraph(labelDataset, id); err != nil {
			slog.Warn("failed deleting labels extracted from", "id", id)
		}
	}
	return
}

// checkGraphExists asks the dataset whether a named graph exists.
// It returns a boolean flag and any error encountered.
func checkGraphExists(dataset string, id string) (exists bool, err error) {
	// prevent SPARQL injection
	if !isValidIRI(id) {
		return false, fmt.Errorf("invalid id IRI: %v", id)
	}
	exists = false
	body := url.Values{}
	body.Set("query", fmt.Sprintf("ASK WHERE { GRAPH <%s> { ?s ?p ?o } }", id))
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/%s", FusekiEndpoint, dataset), strings.NewReader(body.Encode()))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	status, data, err := doRequest(req)
	if err != nil {
		return
	}
	if status != http.StatusOK {
		err = newHTTPError(fmt.Sprintf("failed checking graph %s in dataset %s", id, dataset), status, data)
		return
	}
	var response map[string]any
	if err = json.Unmarshal(data, &response); err != nil {
		return
	}
	if val, ok := response["boolean"]; ok {
		if exists, ok = val.(bool); !ok {
			err = fmt.Errorf("got non boolean response")
		}
	}
	return
}

// getAllGraphIds lists graph identifiers in a dataset.
// It returns the slice of graph IDs and any error encountered.
func getAllGraphIds(dataset string) ([]string, error) {
	bindings, err := queryDataset(dataset, "SELECT DISTINCT ?g WHERE { GRAPH ?g { } }")
	if err != nil {
		return nil, err
	}
	res, err := sparql.ParseJSON(bytes.NewReader(bindings))
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, row := range res.Solutions() {
		g, okG := row["g"].(rdf.Context)
		if !okG {
			return nil, fmt.Errorf("invalid binding: %v", row)
		}
		ids = append(ids, g.String())
	}
	return ids, nil
}

// queryDataset executes a SPARQL query and returns the JSON response bytes.
// It returns the raw response data and any error encountered.
func queryDataset(dataset string, query string) (data []byte, err error) {
	body := url.Values{}
	body.Set("query", query)
	req, err := http.NewRequest("POST", fmt.Sprintf("%s/%s", FusekiEndpoint, dataset), strings.NewReader(body.Encode()))
	if err != nil {
		return
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	status, responseBody, err := doRequest(req)
	if err != nil {
		return
	}
	if status != http.StatusOK {
		err = newHTTPError(fmt.Sprintf(`failed querying dataset "%s" with query: %s`, dataset, query), status, responseBody)
		return
	}
	data = responseBody
	return
}

// updateDataset executes a SPARQL update query.
// It returns an error if the update request fails.
func updateDataset(dataset string, query string) (err error) {
	form := url.Values{}
	form.Set("update", query)
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/%s/update", FusekiEndpoint, dataset), strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", AuthHeader)
	status, responseBody, err := doRequest(req)
	if err != nil {
		return
	}
	if !statusIsOK(status) {
		err = newHTTPError(fmt.Sprintf(`failed updating dataset "%s" with query: %s`, dataset, query), status, responseBody)
	}
	return
}

// sparqlResultToNQuads converts SPARQL JSON results into N-Quads.
// It returns the encoded N-Quads bytes or an error.
func sparqlResultToNQuads(bindings []byte) ([]byte, error) {
	res, err := sparql.ParseJSON(bytes.NewReader(bindings))
	if err != nil {
		return nil, err
	}
	var result bytes.Buffer
	enc := rdf.NewQuadEncoder(&result, rdf.NQuads)

	for _, row := range res.Solutions() {
		s, okS := row["s"].(rdf.Subject)
		p, okP := row["p"].(rdf.Predicate)
		o, okO := row["o"].(rdf.Object)
		g, okG := row["g"].(rdf.Context)
		if !okS || !okP || !okO || !okG {
			return nil, fmt.Errorf("invalid quad: %v", row)
		}
		if err := enc.Encode(rdf.Quad{Triple: rdf.Triple{Subj: s, Pred: p, Obj: o}, Ctx: g}); err != nil {
			return nil, err
		}
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return result.Bytes(), nil
}
