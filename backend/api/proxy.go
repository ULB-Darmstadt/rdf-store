package api

import (
	"log/slog"
	"net/http"
	"rdf-store-backend/base"
	"rdf-store-backend/sparql"
	"strings"

	"github.com/gin-gonic/gin"
)

func init() {
	Router.GET(BasePath+"/proxy", handleHttpProxy)
}

var allowedContentTypes = []string{"text/turtle", "application/trig", "application/n-triples", "application/n-quads", "text/n3", "application/ld+json"}

func intersectPreserveOrder[T comparable](a, b []T) []T {
	setB := make(map[T]struct{}, len(b))
	for _, v := range b {
		setB[v] = struct{}{}
	}

	var result []T
	for _, v := range a {
		if _, ok := setB[v]; ok {
			result = append(result, v)
		}
	}
	return result
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
			accept := strings.Join(intersectPreserveOrder(allowedContentTypes, strings.Split(c.Request.Header.Get("Accept"), ",")), ",")
			data, err = base.CacheLoad(url, accept)
			if err != nil {
				slog.Error("failed proxying", "url", url, "error", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
		}
	}
	c.Writer.Write(data)
}
