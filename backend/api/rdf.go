package api

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"rdf-store-backend/base"
	"rdf-store-backend/rdf"
	"rdf-store-backend/search"
	"strings"

	"github.com/gin-gonic/gin"
)

var fusekiProxy *httputil.ReverseProxy
var fusekiProxyTarget *url.URL

// init configures the Fuseki proxy and registers RDF API routes.
func init() {
	var err error
	// init fuseki proxy
	fusekiProxyTarget, err = url.Parse(rdf.FusekiEndpoint)
	if err != nil {
		panic(err)
	}
	fusekiProxy = httputil.NewSingleHostReverseProxy(fusekiProxyTarget)
	fusekiProxy.ModifyResponse = func(resp *http.Response) error {
		// delete CORS headers sent by the sparql endpoint. we're setting these ourselves in the http handler chain.
		// not deleting the headers will produce e.g. "CORS Multiple Origin Not Allowed" errors in the browser.
		resp.Header.Del("Access-Control-Allow-Origin")
		resp.Header.Del("Access-Control-Allow-Credentials")
		return nil
	}

	Router.GET(BasePath+"/resource/*id", handleGetResource)
	Router.POST(BasePath+"/resource", handleAddResource)
	Router.PUT(BasePath+"/resource/*id", handleUpdateResource)
	Router.DELETE(BasePath+"/resource/*id", handleDeleteResource)
	Router.GET(BasePath+"/profile/*id", handleGetProfile)
	Router.POST(BasePath+"/sparql/query", handleFusekiSparql)
	Router.GET(BasePath+"/sparql/query", handleFusekiSparql)
	Router.GET(BasePath+"/class-instances", handleGetClassInstances)
	Router.GET(BasePath+"/shape-instances", handleGetShapeInstances)
	if base.ExposeFusekiFrontend {
		Router.Any("/fuseki/*proxyPath", handleFusekiFrontend)
	}
}

// handleFusekiSparql proxies SPARQL queries to Fuseki with auth header.
func handleFusekiSparql(c *gin.Context) {
	c.Request.URL.Path = fmt.Sprintf("/%s/query", rdf.ResourceDataset)
	c.Request.URL.Scheme = fusekiProxyTarget.Scheme
	c.Request.URL.Host = fusekiProxyTarget.Host
	c.Request.Host = fusekiProxyTarget.Host
	c.Request.Header.Set("X-Forwarded-Host", c.Request.Header.Get("Host"))
	c.Request.Header.Set("Authorization", rdf.AuthHeader)
	fusekiProxy.ServeHTTP(c.Writer, c.Request)
}

// handleFusekiFrontend proxies the Fuseki UI when exposed.
func handleFusekiFrontend(c *gin.Context) {
	c.Request.URL.Path = fusekiProxyTarget.Path + c.Param("proxyPath")
	c.Request.URL.Scheme = fusekiProxyTarget.Scheme
	c.Request.URL.Host = fusekiProxyTarget.Host
	c.Request.Host = fusekiProxyTarget.Host
	c.Request.Header.Set("X-Forwarded-Host", c.Request.Header.Get("Host"))
	fusekiProxy.ServeHTTP(c.Writer, c.Request)
}

// handleGetResource retrieves a resource and returns it as Turtle.
func handleGetResource(c *gin.Context) {
	id := c.Param("id")
	did, err := url.QueryUnescape(id)
	if err != nil {
		slog.Error("failed unescaping parameter", "param", id, "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	did = strings.TrimPrefix(did, "/")
	// if "includeLinked" request parameter is set, then pull in linked resources
	includeLinked := c.Request.URL.Query().Has("includeLinked")
	resource, metadata, err := rdf.GetResource(did, includeLinked)
	if err != nil {
		slog.Error("failed loading resource", "id", did, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if metadata != nil && metadata.Creator != "" {
		c.Header("X-Creator", metadata.Creator)
	}
	c.Data(http.StatusOK, "text/turtle", resource)
}

// handleAddResource validates and stores a new RDF resource.
func handleAddResource(c *gin.Context) {
	granted, user := writeAccessGranted(c.Request.Header)
	if !granted {
		c.JSON(http.StatusForbidden, gin.H{"error": "not allowed"})
		return

	}
	data, err := readGraphBytesFromRequest(c)
	if err != nil {
		slog.Error("failed loading graph from request", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	resource, metadata, err := rdf.CreateResource(data, user)
	if err != nil {
		slog.Error("failed creating resource", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err = search.IndexResource(resource, metadata); err != nil {
		slog.Error("failed indexing resource", "id", metadata.Id.RawValue(), "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Header("Location", metadata.Id.RawValue())
	c.String(http.StatusNoContent, "")
}

// handleUpdateResource validates and updates an existing RDF resource.
func handleUpdateResource(c *gin.Context) {
	granted, user := writeAccessGranted(c.Request.Header)
	if !granted {
		c.JSON(http.StatusForbidden, gin.H{"error": "not allowed"})
		return
	}
	id := c.Param("id")
	did, err := url.QueryUnescape(id)
	if err != nil {
		slog.Error("failed unescaping parameter", "param", id, "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	did = strings.TrimPrefix(did, "/")

	data, err := readGraphBytesFromRequest(c)
	if err != nil {
		slog.Error("failed loading graph from request", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	resource, metadata, err := rdf.UpdateResource(did, data, user)
	if err != nil {
		slog.Error("failed updating resource", "id", did, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err = search.IndexResource(resource, metadata); err != nil {
		slog.Error("failed indexing resource", "id", did, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.String(http.StatusNoContent, "")
}

// handleDeleteResource deletes a resource and updates the search index.
func handleDeleteResource(c *gin.Context) {
	granted, user := writeAccessGranted(c.Request.Header)
	if !granted {
		c.JSON(http.StatusForbidden, gin.H{"error": "not allowed"})
		return
	}
	id := c.Param("id")
	did, err := url.QueryUnescape(id)
	if err != nil {
		slog.Error("failed unescaping parameter", "param", id, "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	did = strings.TrimPrefix(did, "/")
	if err := rdf.DeleteResource(did, user); err != nil {
		slog.Error("failed deleting resource", "id", did, "error", err)
		if errors.Is(err, rdf.ErrResourceLinked) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err = search.DeindexResource(did); err != nil {
		slog.Error("failed deindexing resource", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.String(http.StatusNoContent, "")
}

// handleGetProfile returns a profile graph in Turtle.
func handleGetProfile(c *gin.Context) {
	id := c.Param("id")
	did, err := url.QueryUnescape(id)
	if err != nil {
		slog.Error("failed unescaping parameter", "param", id, "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	did = strings.TrimPrefix(did, "/")
	graph, err := rdf.GetProfile(did)
	if err != nil {
		slog.Error("failed loading profile", "id", did, "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "text/turtle", graph)
}

// handleGetClassInstances returns instances of a given RDF class.
func handleGetClassInstances(c *gin.Context) {
	class := c.Query("class")
	if len(class) == 0 {
		slog.Warn("failed retrieving class instances, request parameter 'class' missing")
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing request parameter 'class'"})
		return

	}
	instances, err := rdf.GetClassInstances(class)
	if err != nil {
		slog.Error("failed retrieving class instances", "class", class, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "text/turtle", instances)
}

// handleGetShapeInstances returns all instances that conform to a given SHACL shape.
func handleGetShapeInstances(c *gin.Context) {
	shape := c.Query("shape")
	if len(shape) == 0 {
		slog.Warn("failed retrieving shape instances, request parameter 'shape' missing")
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing request parameter 'shape'"})
		return

	}
	instances, err := rdf.GetShapeInstances(shape)
	if err != nil {
		slog.Error("failed retrieving shape instances", "shape", shape, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "text/turtle", instances)
}

// readGraphBytesFromRequest reads raw RDF Turtle bytes from a form parameter.
func readGraphBytesFromRequest(c *gin.Context) (data []byte, err error) {
	if ttl := c.PostForm("ttl"); ttl != "" {
		data = []byte(ttl)
	} else {
		err = errors.New("no ttl form param")
	}
	return
}
