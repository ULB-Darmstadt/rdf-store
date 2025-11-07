package api

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"rdf-store-backend/base"
	"rdf-store-backend/search"
	"rdf-store-backend/shacl"
	"rdf-store-backend/sparql"
	"strings"

	"github.com/deiu/rdf2go"
	"github.com/gin-gonic/gin"
)

var fusekiProxy *httputil.ReverseProxy
var fusekiProxyTarget *url.URL

func init() {
	var err error
	// init fuseki proxy
	fusekiProxyTarget, err = url.Parse(sparql.Endpoint)
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

	Router.GET(basePath+"/resource/*id", handleGetResource)
	Router.POST(basePath+"/resource", handleAddResource)
	Router.PUT(basePath+"/resource/*id", handleUpdateResource)
	Router.DELETE(basePath+"/resource/*id", handleDeleteResource)
	Router.GET(basePath+"/profile/*id", handleGetProfile)
	Router.POST(basePath+"/sparql/query", handleFusekiSparql)
	Router.GET(basePath+"/sparql/query", handleFusekiSparql)
	if base.ExposeFusekiFrontend {
		Router.Any("/fuseki/*proxyPath", handleFusekiFrontend)
	}
}

func handleFusekiSparql(c *gin.Context) {
	c.Request.URL.Path = fmt.Sprintf("/%s/query", sparql.ResourceDataset)
	c.Request.URL.Scheme = fusekiProxyTarget.Scheme
	c.Request.URL.Host = fusekiProxyTarget.Host
	c.Request.Host = fusekiProxyTarget.Host
	c.Request.Header.Set("X-Forwarded-Host", c.Request.Header.Get("Host"))
	c.Request.Header.Set("Authorization", sparql.AuthHeader)
	fusekiProxy.ServeHTTP(c.Writer, c.Request)
}

func handleFusekiFrontend(c *gin.Context) {
	c.Request.URL.Path = fusekiProxyTarget.Path + c.Param("proxyPath")
	c.Request.URL.Scheme = fusekiProxyTarget.Scheme
	c.Request.URL.Host = fusekiProxyTarget.Host
	c.Request.Host = fusekiProxyTarget.Host
	c.Request.Header.Set("X-Forwarded-Host", c.Request.Header.Get("Host"))
	fusekiProxy.ServeHTTP(c.Writer, c.Request)
}

func handleGetResource(c *gin.Context) {
	id := c.Param("id")
	did, err := url.QueryUnescape(id)
	if err != nil {
		slog.Error("failed unescaping parameter", "param", id, "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	did = strings.TrimPrefix(did, "/")
	resource, _, err := sparql.LoadResource(did, false)
	if err != nil {
		slog.Error("failed loading resource", "id", did, "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "text/turtle", resource)
}

func handleAddResource(c *gin.Context) {
	granted, user := writeAccessGranted(c.Request.Header)
	if !granted {
		c.JSON(http.StatusForbidden, gin.H{"error": "not allowed"})
		return

	}
	graph, err := readGraphFromRequest(c)
	if err != nil {
		slog.Error("failed loading graph from request", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	resourceID, profile, err := sparql.FindResourceProfile(graph, nil)
	dataGraph, _ := readGraphBytesFromRequest(c)
	shapesGraph, err := sparql.LoadProfile(profile.Id.RawValue())
	if err != nil {
		slog.Error("failed loading shapes graph", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := shacl.Validate(string(shapesGraph), profile.Id.RawValue(), string(dataGraph), resourceID.RawValue()); err != nil {
		slog.Error("failed validating graph", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	metadata, err := sparql.CreateResource(resourceID.RawValue(), dataGraph, user)
	if err != nil {
		slog.Error("failed creating resource", "id", resourceID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err = search.IndexResource(resourceID, profile, graph, metadata); err != nil {
		slog.Error("failed indexing resource", "id", resourceID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.String(http.StatusNoContent, "")
}

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

	graph, err := readGraphFromRequest(c)
	if err != nil {
		slog.Error("failed loading graph from request", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	resourceID := rdf2go.NewResource(did)
	_, profile, err := sparql.FindResourceProfile(graph, &resourceID)
	profileID := profile.Id.RawValue()
	dataGraph, _ := readGraphBytesFromRequest(c)
	shapesGraph, err := sparql.LoadProfile(profileID)
	if err != nil {
		slog.Error("failed loading shapes graph", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := shacl.Validate(string(shapesGraph), profileID, string(dataGraph), did); err != nil {
		slog.Error("failed validating graph", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	data, _ := readGraphBytesFromRequest(c)
	metadata, err := sparql.UpdateResource(did, data, user)
	if err != nil {
		slog.Error("failed updating resource", "id", did, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err = search.IndexResource(resourceID, profile, graph, metadata); err != nil {
		slog.Error("failed indexing resource", "id", did, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.String(http.StatusNoContent, "")
}

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
	if err := sparql.DeleteResource(did, user); err != nil {
		slog.Error("failed deleting resource", "id", did, "error", err)
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

func handleGetProfile(c *gin.Context) {
	id := c.Param("id")
	did, err := url.QueryUnescape(id)
	if err != nil {
		slog.Error("failed unescaping parameter", "param", id, "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	did = strings.TrimPrefix(did, "/")
	graph, err := sparql.LoadProfile(did)
	if err != nil {
		slog.Error("failed loading profile", "id", did, "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "text/turtle", graph)
}

func readGraphFromRequest(c *gin.Context) (graph *rdf2go.Graph, err error) {
	if ttl := c.PostForm("ttl"); ttl != "" {
		graph, err = base.ParseGraph(strings.NewReader(ttl))
	} else {
		err = errors.New("no ttl form param")
	}
	return
}

func readGraphBytesFromRequest(c *gin.Context) (data []byte, err error) {
	if ttl := c.PostForm("ttl"); ttl != "" {
		data = []byte(ttl)
	} else {
		err = errors.New("no ttl form param")
	}
	return
}
