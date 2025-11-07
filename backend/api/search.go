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

func init() {
	var err error
	// init solr proxy
	solrProxyTarget, err = url.Parse(search.Endpoint)
	if err != nil {
		panic(err)
	}
	solrProxy = httputil.NewSingleHostReverseProxy(solrProxyTarget)

	Router.GET(basePath+"/solr/:collection/schema", handleSolr)
	Router.GET(basePath+"/solr/:collection/query", handleSolr)
	Router.POST(basePath+"/solr/:collection/query", handleSolr)
}

func handleSolr(c *gin.Context) {
	c.Request.URL.Path = strings.TrimPrefix(c.Request.URL.Path, basePath)
	c.Request.URL.Scheme = solrProxyTarget.Scheme
	c.Request.URL.Host = solrProxyTarget.Host
	c.Request.Host = solrProxyTarget.Host
	c.Request.Header.Set("X-Forwarded-Host", c.Request.Header.Get("Host"))
	solrProxy.ServeHTTP(c.Writer, c.Request)
}
