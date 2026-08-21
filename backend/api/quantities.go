package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"rdf-store-backend/search/qudt"

	"github.com/gin-gonic/gin"
)

func init() {
	Router.POST(BasePath+"/quantities", handleQuantities)
}

type QuantityUnitConversion struct {
	UnitURI         string         `json:"unitURI"`
	QuantityKindURI string         `json:"quantityKindURI"`
	Conversion      *qudt.UnitInfo `json:"conversion"`
}

func handleQuantities(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		slog.Error("failed parsing quantities", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var quantities []QuantityUnitConversion
	if err = json.Unmarshal(body, &quantities); err != nil {
		slog.Error("failed parsing quantities", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	for i := range quantities {
		quantities[i].Conversion = qudt.Unit(quantities[i].UnitURI)
	}
	c.JSON(http.StatusOK, quantities)
}
