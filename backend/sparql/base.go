package sparql

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
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

var Endpoint = base.EnvVar("FUSEKI_ENDPOINT", "http://localhost:3030")
var AuthHeader = fmt.Sprintf("Basic %s", base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", base.EnvVar("FUSEKI_USER", "admin"), base.EnvVar("FUSEKI_PASSWORD", "secret")))))
var ResourceDataset = base.EnvVar("FUSEKI_RESOURCE_DATASET", "resource")
var profileDataset = base.EnvVar("FUSEKI_PROFILE_DATASET", "profile")
var labelDataset = base.EnvVar("FUSEKI_LABEL_DATASET", "label")

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

func initDatasets() error {
	for _, dataset := range []string{ResourceDataset, profileDataset, labelDataset} {
		// check if dataset exists
		req, err := http.NewRequest("GET", fmt.Sprintf("%s/$/stats/%s", Endpoint, dataset), nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", AuthHeader)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			// dataset does not exist, so create it
			req, err = http.NewRequest(http.MethodPost, fmt.Sprintf("%s/$/datasets?dbName=%s&dbType=TDB2", Endpoint, dataset), nil)
			if err != nil {
				return err
			}
			req.Header.Set("Authorization", AuthHeader)
			// req.Header.Set("Content-Type", "text/turtle")
			resp, err = http.DefaultClient.Do(req)
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			if resp.StatusCode != 200 {
				data, err := io.ReadAll(resp.Body)
				if err != nil {
					return err
				}
				return fmt.Errorf("failed creating dataset. status: %v, response: %v", resp.StatusCode, string(data))
			}
		}
	}
	return nil
}

func importLocalResources() error {
	baseDir := path.Join("local", "datagraph")
	if files, err := os.ReadDir(baseDir); err == nil {
		for _, file := range files {
			if !file.IsDir() && strings.HasSuffix(file.Name(), ".ttl") {
				slog.Info("importing data graph", "file", file.Name())
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

func createGraph(dataset string, id string, data []byte) error {
	if exists, err := checkGraphExists(dataset, id); err != nil || exists {
		return fmt.Errorf("graph %s already exists in dataset %s", id, dataset)
	}
	return uploadGraph(dataset, id, data, nil)
}

func loadGraph(dataset string, id string) (data []byte, err error) {
	if exists, err := checkGraphExists(dataset, id); err != nil || !exists {
		err = fmt.Errorf("graph %s not found in dataset %s", id, dataset)
		return nil, err
	}
	return queryDataset(dataset, fmt.Sprintf(`CONSTRUCT { ?s ?p ?o } WHERE { GRAPH <%s> { ?s ?p ?o } }`, id))
}

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

	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/%s/data?graph=%s", Endpoint, dataset, id), body)
	if err != nil {
		return err
	}
	req.Header.Add("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", AuthHeader)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		message := ""
		if body, err := io.ReadAll(resp.Body); err == nil {
			message = string(body)
		}
		return fmt.Errorf("failed uploading graph %s in dataset %s - status: %v, response: '%v'", id, dataset, resp.StatusCode, message)
	}
	// extract labels from graph
	if dataset != labelDataset {
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

func deleteGraph(dataset string, id string) (err error) {
	err = updateDataset(dataset, fmt.Sprintf(`DROP GRAPH <%s>`, id))
	if err != nil {
		return
	}
	// delete labels that were sourced from the deleted graph
	if dataset != labelDataset {
		err = deleteGraph(labelDataset, id)
	}
	return
}

func checkGraphExists(dataset string, id string) (exists bool, err error) {
	exists = false
	body := url.Values{}
	body.Set("query", fmt.Sprintf("ASK WHERE { GRAPH <%s> { ?s ?p ?o } }", id))
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/%s", Endpoint, dataset), strings.NewReader(body.Encode()))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return
	}
	var response map[string]interface{}
	if err = json.Unmarshal(data, &response); err != nil {
		return
	}
	if val, ok := response["boolean"]; ok {
		exists = val.(bool)
	}
	return
}

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

func queryDataset(dataset string, query string) (data []byte, err error) {
	body := url.Values{}
	body.Set("query", query)
	req, err := http.NewRequest("POST", fmt.Sprintf("%s/%s", Endpoint, dataset), strings.NewReader(body.Encode()))
	if err != nil {
		return
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		message := ""
		if body, err := io.ReadAll(resp.Body); err == nil {
			message = string(body)
		}
		err = fmt.Errorf(`failed querying dataset "%s" - status: %d, query: %s, message: '%s'`, dataset, resp.StatusCode, query, message)
	} else {
		data, err = io.ReadAll(resp.Body)
	}
	return
}

func updateDataset(dataset string, query string) (err error) {
	form := url.Values{}
	form.Set("update", query)
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/%s/update", Endpoint, dataset), strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", AuthHeader)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		message := ""
		if body, err := io.ReadAll(resp.Body); err == nil {
			message = string(body)
		}
		err = fmt.Errorf(`failed updating dataset "%s" - status: %d, query: %s, message: '%s'`, dataset, resp.StatusCode, query, message)
	}
	return
}

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
