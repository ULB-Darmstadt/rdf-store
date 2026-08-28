package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestHandleQuantitiesResolvesConversions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := `[` +
		`{"unitURI":"","quantityKindURI":"http://qudt.org/vocab/quantitykind/Temperature"},` +
		`{"unitURI":"http://qudt.org/vocab/unit/DEG_C","quantityKindURI":"http://qudt.org/vocab/quantitykind/Temperature"},` +
		`{"unitURI":"http://qudt.org/vocab/unit/DEG_C","quantityKindURI":"http://qudt.org/vocab/quantitykind/Temperature","isDelta":true},` +
		`{"unitURI":"http://qudt.org/vocab/unit/FT","quantityKindURI":"http://qudt.org/vocab/quantitykind/Length"},` +
		`{"unitURI":"http://qudt.org/vocab/unit/FT","quantityKindURI":"http://qudt.org/vocab/quantitykind/Temperature"},` +
		`{"unitURI":"http://example.org/nonexistent","quantityKindURI":"http://qudt.org/vocab/quantitykind/Length"}` +
		`]`
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	handleQuantities(context)

	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body)
	}
	var results []QuantityUnitConversion
	if err := json.Unmarshal(response.Body.Bytes(), &results); err != nil {
		t.Fatal(err)
	}
	if len(results) != 6 {
		t.Fatalf("expected 6 results, got %d", len(results))
	}
	if results[0].CanonicalUnitURI != "http://qudt.org/vocab/unit/K" || results[0].Conversion != nil {
		t.Fatalf("unexpected canonical-only result: %#v", results[0])
	}
	celsius := results[1].Conversion
	if celsius == nil || celsius.Multiplier != 1 || celsius.Offset != 273.15 {
		t.Fatalf("unexpected celsius conversion: %#v", results[1].Conversion)
	}
	if results[1].CanonicalUnitURI != "http://qudt.org/vocab/unit/K" {
		t.Fatalf("unexpected celsius canonical unit: %q", results[1].CanonicalUnitURI)
	}
	delta := results[2].Conversion
	if delta == nil || delta.Offset != 0 || delta.Multiplier != 1 {
		t.Fatalf("expected offset-free delta conversion, got %#v", results[2].Conversion)
	}
	feet := results[3].Conversion
	if feet == nil || feet.Multiplier != 0.3048 || feet.Offset != 0 {
		t.Fatalf("unexpected feet conversion: %#v", results[3].Conversion)
	}
	if results[4].Conversion != nil {
		t.Fatalf("expected no conversion for dimensionally incompatible kind, got %#v", results[4].Conversion)
	}
	if results[5].Conversion != nil {
		t.Fatalf("expected no conversion for unknown unit, got %#v", results[5].Conversion)
	}
}

func TestHandleQuantitiesRejectsMalformedBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{invalid"))
	handleQuantities(context)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", response.Code, response.Body)
	}
}
