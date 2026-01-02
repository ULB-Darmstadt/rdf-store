package api

import (
	"log/slog"
	"net/http"
	"path"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v3"
)

var apispec = newApiSpec()

func init() {
	Router.GET(BasePath+"/openapi.json", func(c *gin.Context) {
		setBackendUrl(c)
		c.JSON(http.StatusOK, apispec)
	})

	Router.GET(BasePath+"/openapi.yaml", func(c *gin.Context) {
		setBackendUrl(c)
		data, err := yaml.Marshal(&apispec)
		if err != nil {
			slog.Error("failed marhaling openapi spec", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.Header("Content-Type", "text/yaml")
		c.Writer.Write(data)
	})
}

func setBackendUrl(c *gin.Context) {
	apispec.Servers[0].URL = c.GetHeader("X-Forwarded-For") + path.Dir(c.Request.RequestURI)
}

// newApiSpec instantiates the OpenAPI specification for this service.
func newApiSpec() *openapi3.T {
	return &openapi3.T{
		OpenAPI: "3.1.0",
		Info: &openapi3.Info{
			Title:       "RDF store API",
			Description: "REST API for interacting with RDF store",
			Version:     "0.0.1",
			License: &openapi3.License{
				Name: "MIT License",
				URL:  "https://opensource.org/licenses/MIT",
			},
			// Contact: &openapi3.Contact{
			// 	Name:  "Contact",
			// 	Email: "ingest@nfdi4ing.de",
			// },
		},
		Servers: openapi3.Servers{
			&openapi3.Server{
				Description: "Production",
				// URL:         base.BackendUrl,
			},
		},
		Security: openapi3.SecurityRequirements{
			openapi3.SecurityRequirement{
				"jwt":    []string{},
				"openid": []string{},
			},
		},
		Tags: openapi3.Tags{
			// &openapi3.Tag{Name: TAG_SEARCH},
			// &openapi3.Tag{Name: TAG_DATASET},
			// &openapi3.Tag{Name: TAG_HOMOGENIZATION},
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
}
