package api

import (
	"log/slog"
	"net/http"
	"rdf-store-backend/rdf"

	"github.com/gin-gonic/gin"
)

// init registers the label lookup endpoint.
func init() {
	Router.POST(BasePath+"/labels", handleLabels)
}

// handleLabels resolves labels for provided RDF ids and language.
func handleLabels(c *gin.Context) {
	language := c.PostForm("lang")
	ids := c.PostFormArray("id")
	labels, err := rdf.GetLabels(language, ids)
	if err != nil {
		slog.Error("failed getting labels", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, labels)
}
