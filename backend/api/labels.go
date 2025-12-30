package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"rdf-store-backend/sparql"

	"github.com/gin-gonic/gin"
)

func init() {
	Router.POST(BasePath+"/labels", handleLabels)
}

func handleLabels(c *gin.Context) {
	language := c.PostForm("lang")
	ids := c.PostFormArray("id")
	labels, err := sparql.GetLabels(language, ids)
	if err != nil {
		slog.Error("failed getting labels", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	data, err := json.Marshal(labels)
	if err != nil {
		slog.Error("failed marshalling labels", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Writer.Write(data)
}
