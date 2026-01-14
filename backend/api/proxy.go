package api

import (
	"log/slog"
	"net/http"
	"rdf-store-backend/base"
	"rdf-store-backend/sparql"
	"slices"
	"strings"

	"github.com/gin-gonic/gin"
)

func init() {
	Router.GET(BasePath+"/proxy", handleHttpProxy)
}

var allowedContentTypes = []string{"text/turtle", "application/trig", "application/n-triples", "application/n-quads", "text/n3", "application/ld+json"}

func filterClientAccept(req *http.Request) string {
	var result []string
	for _, mime := range strings.Split(req.Header.Get("Accept"), ",") {
		if slices.Index(allowedContentTypes, strings.Split(mime, ";")[0]) > 0 {
			result = append(result, mime)
		}
	}
	if len(result) == 0 {
		result = allowedContentTypes
	}
	return strings.Join(result, ",")
}

func handleHttpProxy(c *gin.Context) {
	url := c.Query("url")
	if len(url) == 0 {
		slog.Warn("failed proxying, request parameter 'url' missing")
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing request parameter 'url'"})
		return

	}
	var data []byte
	// check if URL references a profile
	if profile, ok := sparql.Profiles[url]; ok {
		data = *profile.RDF
	} else {
		var err error
		// check if URL references a resource
		data, _, err = sparql.GetResource(url, true)
		if err != nil {
			// URL refences no profile or resource, so try to load URL from cache or from the web
			data, err = base.CacheLoad(url, filterClientAccept(c.Request))
			if err != nil {
				slog.Error("failed proxying", "url", url, "error", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
		}
	}
	c.Writer.Write(data)
}
