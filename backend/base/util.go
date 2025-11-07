package base

import (
	"hash/fnv"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"regexp"
	"strings"

	"github.com/deiu/rdf2go"
)

var fixBooleanRegex = regexp.MustCompile(`(true|false)(\s*)]`)

// dirty fix for buggy boolean parsing in rdf2go
func FixBooleansInProfile(profile []byte) []byte {
	return fixBooleanRegex.ReplaceAll(profile, []byte("${1} ; ]"))
}

func ParseGraph(reader io.Reader) (graph *rdf2go.Graph, err error) {
	graph = rdf2go.NewGraph("")
	err = graph.Parse(reader, "text/turtle")
	return
}

func CacheLoad(url string, header *http.Header) ([]byte, error) {
	cacheFilename := path.Join("local", "cache", strings.ReplaceAll(url, "/", "🐴"))
	data, err := os.ReadFile(cacheFilename)
	if err != nil {
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return nil, err
		}
		if header != nil {
			req.Header = *header
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		data, err = io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		// write data to cache
		if err = os.WriteFile(cacheFilename, data, 0600); err != nil {
			log.Printf("failed caching response for url %s. reason: %v", url, err)
		}
	}
	return data, nil
}

func Hash(data []byte) uint32 {
	h := fnv.New32a()
	h.Write(data)
	return h.Sum32()
}
