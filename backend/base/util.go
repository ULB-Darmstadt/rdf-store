package base

import (
	"fmt"
	"hash/fnv"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path"
	"regexp"
	"strings"

	"github.com/deiu/rdf2go"
)

var fixBooleanRegex = regexp.MustCompile(`(true|false)(\s*)]`)

// dirty fix for buggy boolean parsing in rdf2go
// FixBooleansInRDF normalizes boolean list syntax for rdf2go parsing.
func FixBooleansInRDF(profile []byte) []byte {
	return fixBooleanRegex.ReplaceAll(profile, []byte("${1} ; ]"))
}

// ParseGraph parses RDF Turtle content into a graph.
func ParseGraph(reader io.Reader) (graph *rdf2go.Graph, err error) {
	graph = rdf2go.NewGraph("")
	err = graph.Parse(reader, "text/turtle")
	return
}

// CacheLoad retrieves a URL and caches the response body on disk.
func CacheLoad(url string, accept string) ([]byte, error) {
	cacheFilename := path.Join("local", "cache", strings.ReplaceAll(url, "/", "🐴"))
	data, err := os.ReadFile(cacheFilename)
	if err != nil {
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", accept)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			message := ""
			if body, err := io.ReadAll(resp.Body); err == nil {
				message = string(body)
			}
			return nil, fmt.Errorf("failed retrieving URL %s - status: %v, response: '%v'", url, resp.StatusCode, message)
		}
		// remove e.g. "; charset=utf-8" from content type
		contentType := strings.Split(resp.Header.Get("Content-Type"), ";")[0]
		// ignore reponses with no content type or non accepted content types (e.g. HTML error pages) by writing an empty cache file
		if len(contentType) > 0 && strings.Contains(accept, contentType) {
			data, err = io.ReadAll(resp.Body)
			if err != nil {
				return nil, err
			}
		}
		// write data to cache
		if err = os.WriteFile(cacheFilename, data, 0600); err != nil {
			slog.Warn("failed caching response for", "url", url, "error", err)
		}
	}
	return data, nil
}

// Hash computes a stable hash for the provided bytes.
func Hash(data []byte) uint32 {
	h := fnv.New32a()
	h.Write(data)
	return h.Sum32()
}
