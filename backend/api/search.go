package api

import (
	"net/http/httputil"
	"net/url"
	"rdf-store-backend/search"
	"strings"

	"github.com/gin-gonic/gin"
)

var solrProxy *httputil.ReverseProxy
var solrProxyTarget *url.URL

// init wires up the Solr proxy routes for schema and query endpoints.
func init() {
	var err error
	// init solr proxy
	solrProxyTarget, err = url.Parse(search.Endpoint)
	if err != nil {
		panic(err)
	}
	solrProxy = httputil.NewSingleHostReverseProxy(solrProxyTarget)

	Router.GET(BasePath+"/solr/:collection/schema", handleSolr)
	Router.GET(BasePath+"/solr/:collection/select", handleSolr)
	Router.GET(BasePath+"/solr/:collection/query", handleSolr)
	Router.POST(BasePath+"/solr/:collection/query", handleSolr)
}

// handleSolr proxies Solr query and schema requests to the Solr backend.
func handleSolr(c *gin.Context) {
	c.Request.URL.Path = strings.TrimPrefix(c.Request.URL.Path, BasePath)
	c.Request.URL.Scheme = solrProxyTarget.Scheme
	c.Request.URL.Host = solrProxyTarget.Host
	c.Request.Host = solrProxyTarget.Host
	c.Request.Header.Set("X-Forwarded-Host", c.Request.Header.Get("Host"))
	solrProxy.ServeHTTP(c.Writer, c.Request)
}
