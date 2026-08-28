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
	UnitURI          string         `json:"unitURI"`
	QuantityKindURI  string         `json:"quantityKindURI"`
	CanonicalUnitURI string         `json:"canonicalUnitURI,omitempty"`
	IsDelta          bool           `json:"isDelta,omitempty"`
	Conversion       *qudt.UnitInfo `json:"conversion"`
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
		quantities[i].CanonicalUnitURI = qudt.CanonicalUnitURIForKind(quantities[i].QuantityKindURI)
		quantities[i].Conversion = quantityConversion(&quantities[i])
	}
	c.JSON(http.StatusOK, quantities)
}

// quantityConversion resolves the conversion factors applied when the value of
// a measurement node was indexed, mirroring qudt.Convert: no conversion is
// reported unless a dimensionally compatible canonical unit exists for the
// quantity kind, and delta quantities convert without offsets.
func quantityConversion(quantity *QuantityUnitConversion) *qudt.UnitInfo {
	info := qudt.Unit(quantity.UnitURI)
	if info == nil || quantity.CanonicalUnitURI == "" || qudt.CanonicalUnitURI(quantity.UnitURI, quantity.QuantityKindURI) == "" {
		return nil
	}
	if !quantity.IsDelta {
		return info
	}
	delta := *info
	delta.Offset = 0
	return &delta
}
