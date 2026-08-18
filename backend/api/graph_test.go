package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"rdf-store-backend/rdf"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestGraphNeighborhoodReturnsJSONPage(t *testing.T) {
	getNeighborhood := func(subject, direction string, offset, limit int) (*rdf.GraphNeighborhood, error) {
		if subject != "https://example.org/root" || direction != rdf.NeighborhoodIncoming || offset != 5 || limit != 10 {
			t.Fatalf("unexpected request: %q, %q, %d, %d", subject, direction, offset, limit)
		}
		return &rdf.GraphNeighborhood{
			Quads:         "<https://example.org/source> <https://example.org/p> <https://example.org/root> <https://example.org/g> .\n",
			LocalSubjects: []string{"https://example.org/root", "https://example.org/source"},
			Offset:        5,
			Limit:         10,
			Returned:      1,
			HasMore:       true,
			NextOffset:    6,
		}, nil
	}

	gin.SetMode(gin.TestMode)
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = httptest.NewRequest(http.MethodGet, "/?subject=https%3A%2F%2Fexample.org%2Froot&direction=incoming&offset=5&limit=10", nil)
	handleGetGraphNeighborhoodWith(context, getNeighborhood)

	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("unexpected response: %d, %q, %s", response.Code, response.Header().Get("Content-Type"), response.Body)
	}
	var page rdf.GraphNeighborhood
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Returned != 1 || !page.HasMore || page.NextOffset != 6 || len(page.LocalSubjects) != 2 {
		t.Fatalf("unexpected response page: %#v", page)
	}
}

func TestGraphNeighborhoodRejectsInvalidLimit(t *testing.T) {
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = httptest.NewRequest(http.MethodGet, "/?subject=https%3A%2F%2Fexample.org%2Froot&direction=outgoing&limit=nope", nil)
	handleGetGraphNeighborhood(context)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", response.Code, response.Body)
	}
}

func TestOpenAPIDocumentsNeighborhoodJSONAndNoLocalSubjectsEndpoint(t *testing.T) {
	spec := newApiSpec()
	path := spec.Paths.Find("/graph/neighborhood")
	if path == nil || path.Get == nil {
		t.Fatal("graph neighborhood path is missing")
	}
	response := path.Get.Responses.Value("200")
	if response == nil || response.Value == nil || response.Value.Content["application/json"] == nil {
		t.Fatal("graph neighborhood JSON response is missing")
	}
	if spec.Components.Schemas["GraphNeighborhoodResponse"] == nil {
		t.Fatal("graph neighborhood response schema is missing")
	}
	if spec.Paths.Find("/graph/local-subjects") != nil {
		t.Fatal("removed local-subjects endpoint remains in OpenAPI")
	}
}
