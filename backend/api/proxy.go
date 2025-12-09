package api

import (
	"log/slog"
	"net/http"
	"rdf-store-backend/base"
	"rdf-store-backend/sparql"

	"github.com/gin-gonic/gin"
)

func init() {
	Router.GET(basePath+"/proxy", handleHttpProxy)
}

func handleHttpProxy(c *gin.Context) {
	url := c.Query("url")
	if len(url) == 0 {
		slog.Warn("failed proxying, request parameter 'url' missing")
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing request parameter 'url'"})
		return

	}
	var data []byte
	// check if url references a profile
	if profile, ok := sparql.Profiles[url]; ok {
		data = *profile.RDF
	} else {
		var err error
		// check if url references a resource
		data, _, err = sparql.LoadResource(url, false)
		if err != nil {
			// url refences no profile or resource, so try to load URL from cache or from the web
			data, err = base.CacheLoad(url, &c.Request.Header)
			if err != nil {
				slog.Error("failed proxying", "url", url, "error", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
		}
	}
	c.Writer.Write(data)
}
