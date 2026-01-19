package api

import (
	"log/slog"
	"net/http"
	"rdf-store-backend/base"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v3"
)

var TAG_RDF = "RDF"
var TAG_SOLR = "Search Index"
var TAG_MISC = "Misc"
var apispec = newApiSpec()

// init registers endpoints for OpenAPI JSON and YAML specs.
func init() {
	Router.GET(BasePath+"/openapi.json", func(c *gin.Context) {
		c.JSON(http.StatusOK, apispec)
	})

	Router.GET(BasePath+"/openapi.yaml", func(c *gin.Context) {
		data, err := yaml.Marshal(apispec)
		if err != nil {
			slog.Error("failed marhaling openapi spec", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.Header("Content-Type", "text/yaml")
		c.Writer.Write(data)
	})
}

// newApiSpec constructs the OpenAPI specification for this service.
func newApiSpec() *openapi3.T {
	spec := &openapi3.T{
		OpenAPI: "3.1.0",
		Info: &openapi3.Info{
			Title:       "RDF store API",
			Description: "API for interacting with RDF store",
			Version:     "v1",
			License: &openapi3.License{
				Name: "MIT License",
				URL:  "https://opensource.org/licenses/MIT",
			},
		},
		Servers: openapi3.Servers{
			&openapi3.Server{
				Description: "Production",
				URL:         strings.TrimSuffix(base.BackendUrl, "/") + BasePath,
			},
		},
		Security: openapi3.SecurityRequirements{
			openapi3.SecurityRequirement{
				"jwt":    []string{},
				"openid": []string{},
			},
		},
		Tags: openapi3.Tags{
			&openapi3.Tag{Name: TAG_RDF},
		},
		Components: &openapi3.Components{
			// SecuritySchemes: openapi3.SecuritySchemes{
			// 	"jwt": &openapi3.SecuritySchemeRef{Value: openapi3.NewJWTSecurityScheme()},
			// 	"openid": &openapi3.SecuritySchemeRef{Value: &openapi3.SecurityScheme{
			// 		Type:             "openIdConnect",
			// 		OpenIdConnectUrl: fmt.Sprintf("%s/.well-known/openid-configuration", core.AuthEndpoint),
			// 	}},
			// },
			Schemas:       openapi3.Schemas{},
			RequestBodies: openapi3.RequestBodies{},
			Responses: openapi3.ResponseBodies{
				"ErrorResponse": &openapi3.ResponseRef{
					Value: openapi3.NewResponse().
						WithDescription("Response when errors happen.").
						WithContent(openapi3.NewContentWithJSONSchema(openapi3.NewSchema().
							WithProperty("error", openapi3.NewStringSchema()))),
				},
			},
		},
		Paths: &openapi3.Paths{},
	}
	if len(base.Configuration.ContactEmail) > 0 {
		spec.Info.Contact = &openapi3.Contact{
			Name:  base.Configuration.ContactEmail,
			Email: base.Configuration.ContactEmail,
		}
	}
	addSchemas(spec)
	addPaths(spec)
	return spec
}

// addSchemas registers common schemas used across API responses and requests.
func addSchemas(spec *openapi3.T) {
	spec.Components.Schemas["AuthenticatedConfig"] = openapi3.NewSchemaRef("", openapi3.NewSchema().
		WithProperty("layout", openapi3.NewStringSchema()).
		WithProperty("profiles", openapi3.NewArraySchema().WithItems(openapi3.NewStringSchema())).
		WithProperty("index", openapi3.NewStringSchema()).
		WithProperty("geoDataType", openapi3.NewStringSchema()).
		WithProperty("solrMaxAggregations", openapi3.NewIntegerSchema()).
		WithProperty("authEnabled", openapi3.NewBoolSchema()).
		WithProperty("authWriteAccess", openapi3.NewBoolSchema()).
		WithProperty("authUser", openapi3.NewStringSchema()).
		WithProperty("authEmail", openapi3.NewStringSchema()).
		WithProperty("contactEmail", openapi3.NewStringSchema()).
		WithProperty("rdfNamespace", openapi3.NewStringSchema()))
	spec.Components.Schemas["LabelsResponse"] = openapi3.NewSchemaRef("", openapi3.NewSchema().
		WithAdditionalProperties(openapi3.NewStringSchema()))
	spec.Components.Schemas["Error"] = openapi3.NewSchemaRef("", openapi3.NewSchema().
		WithProperty("error", openapi3.NewStringSchema()))
}

// addPaths defines OpenAPI path items for API endpoints.
func addPaths(spec *openapi3.T) {
	spec.Paths.Set("/config", &openapi3.PathItem{Get: &openapi3.Operation{
		Summary:     "Get runtime configuration",
		OperationID: "getConfig",
		Responses: responses(map[string]*openapi3.Response{
			"200": jsonSchemaResponse(openapi3.NewSchemaRef("#/components/schemas/AuthenticatedConfig", nil), "OK"),
			"500": errorResponse(),
		}),
		Tags: []string{TAG_MISC},
	}})

	spec.Paths.Set("/labels", &openapi3.PathItem{Post: &openapi3.Operation{
		Summary:     "Resolve labels for RDF ids",
		OperationID: "getLabels",
		RequestBody: &openapi3.RequestBodyRef{Value: formRequestBody("lang", "id")},
		Responses: responses(map[string]*openapi3.Response{
			"200": jsonSchemaResponse(openapi3.NewSchemaRef("#/components/schemas/LabelsResponse", nil), "OK"),
			"500": errorResponse(),
		}),
		Tags: []string{TAG_RDF},
	}})

	spec.Paths.Set("/resource", &openapi3.PathItem{Post: &openapi3.Operation{
		Summary:     "Create a new RDF resource",
		OperationID: "createResource",
		RequestBody: &openapi3.RequestBodyRef{Value: formRequestBody("ttl")},
		Responses: responses(map[string]*openapi3.Response{
			"204": openapi3.NewResponse().WithDescription("Created"),
			"400": errorResponse(),
			"403": errorResponse(),
			"500": errorResponse(),
		}),
		Tags: []string{TAG_RDF},
	}})

	resourceItem := &openapi3.PathItem{
		Get: &openapi3.Operation{
			Summary:     "Fetch RDF resource",
			OperationID: "getResource",
			Parameters:  openapi3.Parameters{pathParam("id")},
			Responses: responses(map[string]*openapi3.Response{
				"200": turtleResponse(),
				"400": errorResponse(),
				"500": errorResponse(),
			}),
			Tags: []string{TAG_RDF},
		},
		Put: &openapi3.Operation{
			Summary:     "Update RDF resource",
			OperationID: "updateResource",
			Parameters:  openapi3.Parameters{pathParam("id")},
			RequestBody: &openapi3.RequestBodyRef{Value: formRequestBody("ttl")},
			Responses: responses(map[string]*openapi3.Response{
				"204": openapi3.NewResponse().WithDescription("Updated"),
				"400": errorResponse(),
				"403": errorResponse(),
				"500": errorResponse(),
			}),
			Tags: []string{TAG_RDF},
		},
		Delete: &openapi3.Operation{
			Summary:     "Delete RDF resource",
			OperationID: "deleteResource",
			Parameters:  openapi3.Parameters{pathParam("id")},
			Responses: responses(map[string]*openapi3.Response{
				"204": openapi3.NewResponse().WithDescription("Deleted"),
				"403": errorResponse(),
				"500": errorResponse(),
			}),
			Tags: []string{TAG_RDF},
		},
	}
	spec.Paths.Set("/resource/{id}", resourceItem)

	spec.Paths.Set("/profile/{id}", &openapi3.PathItem{Get: &openapi3.Operation{
		Summary:     "Fetch RDF profile graph",
		OperationID: "getProfile",
		Parameters:  openapi3.Parameters{pathParam("id")},
		Responses: responses(map[string]*openapi3.Response{
			"200": turtleResponse(),
			"400": errorResponse(),
		}),
		Tags: []string{TAG_RDF},
	}})

	spec.Paths.Set("/instances", &openapi3.PathItem{Get: &openapi3.Operation{
		Summary:     "List instances of an RDF class",
		OperationID: "getClassInstances",
		Parameters: openapi3.Parameters{&openapi3.ParameterRef{
			Value: openapi3.NewQueryParameter("class").WithRequired(true),
		}},
		Responses: responses(map[string]*openapi3.Response{
			"200": turtleResponse(),
			"400": errorResponse(),
			"500": errorResponse(),
		}),
		Tags: []string{TAG_RDF},
	}})

	spec.Paths.Set("/sparql/query", &openapi3.PathItem{
		Get: &openapi3.Operation{
			Summary:     "SPARQL GET queries on RDF resources dataset",
			OperationID: "sparqlQueryGet",
			Parameters: openapi3.Parameters{&openapi3.ParameterRef{
				Value: openapi3.NewQueryParameter("query").WithRequired(true),
			}},
			Responses: responses(map[string]*openapi3.Response{
				"200": openapi3.NewResponse().WithDescription("SPARQL response"),
			}),
			Tags: []string{TAG_RDF},
		},
		Post: &openapi3.Operation{
			Summary:     "SPARQL POST queries on RDF resources dataset",
			OperationID: "sparqlQueryPost",
			RequestBody: &openapi3.RequestBodyRef{Value: formRequestBody("query")},
			Responses: responses(map[string]*openapi3.Response{
				"200": openapi3.NewResponse().WithDescription("SPARQL response"),
			}),
			Tags: []string{TAG_RDF},
		},
	})

	spec.Paths.Set("/rdfproxy", &openapi3.PathItem{Get: &openapi3.Operation{
		Summary:     "Proxy RDF content by URL",
		OperationID: "rdfProxy",
		Parameters: openapi3.Parameters{
			&openapi3.ParameterRef{
				Value: openapi3.NewQueryParameter("url").WithRequired(true),
			},
			rdfProxyAcceptHeaderParam(),
		},
		Responses: responses(map[string]*openapi3.Response{
			"200": openapi3.NewResponse().WithDescription("RDF content"),
			"400": errorResponse(),
			"500": errorResponse(),
		}),
		Tags: []string{TAG_RDF},
	}})

	spec.Paths.Set("/solr/{collection}/schema", &openapi3.PathItem{Get: &openapi3.Operation{
		Summary:     "Proxy Solr schema request",
		OperationID: "solrSchema",
		Parameters:  openapi3.Parameters{pathParam("collection")},
		Responses: responses(map[string]*openapi3.Response{
			"200": openapi3.NewResponse().WithDescription("Solr schema response"),
			"500": errorResponse(),
		}),
		Tags: []string{TAG_SOLR},
	}})

	spec.Paths.Set("/solr/{collection}/select", &openapi3.PathItem{Get: &openapi3.Operation{
		Summary:     "Proxy Solr select request",
		OperationID: "solrSelect",
		Parameters:  openapi3.Parameters{pathParam("collection")},
		Responses: responses(map[string]*openapi3.Response{
			"200": openapi3.NewResponse().WithDescription("Solr select response"),
			"500": errorResponse(),
		}),
		Tags: []string{TAG_SOLR},
	}})

	spec.Paths.Set("/solr/{collection}/query", &openapi3.PathItem{
		Get: &openapi3.Operation{
			Summary:     "Proxy Solr query request",
			OperationID: "solrQueryGet",
			Parameters: openapi3.Parameters{&openapi3.ParameterRef{
				Value: openapi3.NewQueryParameter("query").WithRequired(true),
			}, pathParam("collection")},
			Responses: responses(map[string]*openapi3.Response{
				"200": openapi3.NewResponse().WithDescription("Solr query response"),
				"500": errorResponse(),
			}),
			Tags: []string{TAG_SOLR},
		},
		Post: &openapi3.Operation{
			Summary:     "Proxy Solr query request",
			OperationID: "solrQueryPost",
			RequestBody: &openapi3.RequestBodyRef{Value: formRequestBody("query")},
			Parameters:  openapi3.Parameters{pathParam("collection")},
			Responses: responses(map[string]*openapi3.Response{
				"200": openapi3.NewResponse().WithDescription("Solr query response"),
				"500": errorResponse(),
			}),
			Tags: []string{TAG_SOLR},
		},
	})
}

func formRequestBody(fields ...string) *openapi3.RequestBody {
	schema := openapi3.NewSchema()
	for _, field := range fields {
		schema.WithProperty(field, openapi3.NewStringSchema())
	}
	return openapi3.NewRequestBody().
		WithRequired(true).
		WithContent(openapi3.NewContentWithSchema(schema, []string{"application/x-www-form-urlencoded"}))
}

func turtleResponse() *openapi3.Response {
	return openapi3.NewResponse().
		WithDescription("Turtle response").
		WithContent(openapi3.NewContentWithSchema(openapi3.NewStringSchema(), []string{"text/turtle"}))
}

func errorResponse() *openapi3.Response {
	return openapi3.NewResponse().
		WithDescription("Error response").
		WithContent(openapi3.NewContentWithJSONSchemaRef(openapi3.NewSchemaRef("#/components/schemas/Error", nil)))
}

func jsonSchemaResponse(schema *openapi3.SchemaRef, description string) *openapi3.Response {
	return openapi3.NewResponse().
		WithDescription(description).
		WithContent(openapi3.NewContentWithJSONSchemaRef(schema))
}

func responses(items map[string]*openapi3.Response) *openapi3.Responses {
	out := openapi3.NewResponses()
	for code, response := range items {
		out.Set(code, &openapi3.ResponseRef{Value: response})
	}
	return out
}

func pathParam(name string) *openapi3.ParameterRef {
	return &openapi3.ParameterRef{
		Value: openapi3.NewPathParameter(name).WithRequired(true),
	}
}

func rdfProxyAcceptHeaderParam() *openapi3.ParameterRef {
	values := make([]interface{}, 0, len(allowedContentTypes))
	for _, value := range allowedContentTypes {
		values = append(values, value)
	}
	return &openapi3.ParameterRef{
		Value: openapi3.NewHeaderParameter("Accept").
			WithDescription("Allowed RDF content types for rdfproxy responses.").
			WithSchema(openapi3.NewStringSchema().WithEnum(values...)),
	}
}
