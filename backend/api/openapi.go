package api

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
)

func init() {
	Router.GET(BasePath+"/openapi.json", handleOpenAPI)
}

func handleOpenAPI(c *gin.Context) {
	labels := []string{"1", "2"}
	data, err := json.Marshal(labels)
	if err != nil {
		slog.Error("failed marshalling labels", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Writer.Write(data)
}
