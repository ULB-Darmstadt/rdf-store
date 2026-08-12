package api

import (
	"errors"
	"net/http"
	"rdf-store-backend/rdf"
	"strconv"

	"github.com/gin-gonic/gin"
)

const defaultNeighborhoodLimit = 25

func init() {
	Router.GET(BasePath+"/graph/neighborhood", handleGetGraphNeighborhood)
}

func handleGetGraphNeighborhood(c *gin.Context) {
	subject := c.Query("subject")
	direction := c.Query("direction")
	offset, err := queryInt(c, "offset", 0)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	limit, err := queryInt(c, "limit", defaultNeighborhoodLimit)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	data, total, err := rdf.GetGraphNeighborhood(subject, direction, offset, limit)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, rdf.ErrInvalidNeighborhoodRequest) {
			status = http.StatusBadRequest
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.Header("X-Total-Count", strconv.Itoa(total))
	c.Header("X-Offset", strconv.Itoa(offset))
	c.Header("X-Limit", strconv.Itoa(limit))
	c.Data(http.StatusOK, "application/n-quads", data)
}

func queryInt(c *gin.Context, name string, fallback int) (int, error) {
	value := c.Query(name)
	if value == "" {
		return fallback, nil
	}
	return strconv.Atoi(value)
}
