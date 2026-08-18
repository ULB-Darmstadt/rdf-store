package api

import (
	"errors"
	"net/http"
	"rdf-store-backend/rdf"
	"strconv"

	"github.com/gin-gonic/gin"
)

const defaultNeighborhoodLimit = 25

type graphNeighborhoodGetter func(subject, direction string, offset, limit int) (*rdf.GraphNeighborhood, error)

func init() {
	Router.GET(BasePath+"/graph/neighborhood", handleGetGraphNeighborhood)
}

func handleGetGraphNeighborhood(c *gin.Context) {
	handleGetGraphNeighborhoodWith(c, rdf.GetGraphNeighborhood)
}

func handleGetGraphNeighborhoodWith(c *gin.Context, getNeighborhood graphNeighborhoodGetter) {
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

	page, err := getNeighborhood(subject, direction, offset, limit)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, rdf.ErrInvalidNeighborhoodRequest) {
			status = http.StatusBadRequest
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, page)
}

func queryInt(c *gin.Context, name string, fallback int) (int, error) {
	value := c.Query(name)
	if value == "" {
		return fallback, nil
	}
	return strconv.Atoi(value)
}
