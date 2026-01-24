package api

import (
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"rdf-store-backend/base"
	"rdf-store-backend/rdf"
	"slices"
	"strings"

	"github.com/gin-gonic/gin"
)

var allowedContentTypes = []string{"text/turtle", "application/trig", "application/n-triples", "application/n-quads", "text/n3", "application/ld+json"}

// init registers the RDF proxy endpoint.
func init() {
	Router.GET(BasePath+"/rdfproxy", handleRdfProxy)
}

// handleRdfProxy proxies RDF content after validating target URLs.
func handleRdfProxy(c *gin.Context) {
	url := c.Query("url")
	if len(url) == 0 {
		slog.Warn("failed proxying, request parameter 'url' missing")
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing request parameter 'url'"})
		return

	}

	if err := isSafeURL(url); err != nil {
		slog.Error("deny proxying", "url", url, "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	var data []byte
	// check if URL references a profile
	if profile, ok := rdf.Profiles[url]; ok {
		data = *profile.RDF
	} else {
		var err error
		// check if URL references a resource
		data, _, err = rdf.GetResource(url, true)
		if err != nil {
			// URL refences no profile or resource, so try to load URL from cache or from the web
			data, err = base.CacheLoad(url, filterClientAccept(c.Request))
			if err != nil {
				slog.Error("failed proxying", "url", url, "error", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
		}
	}
	c.Writer.Write(data)
}

// filterClientAccept narrows Accept headers to RDF-friendly content types.
func filterClientAccept(req *http.Request) string {
	var result []string
	for mime := range strings.SplitSeq(req.Header.Get("Accept"), ",") {
		if slices.Contains(allowedContentTypes, strings.Split(mime, ";")[0]) {
			result = append(result, mime)
		}
	}
	if len(result) == 0 {
		result = allowedContentTypes
	}
	return strings.Join(result, ",")
}

// isSafeURL validates that a URL resolves to public, globally routable IPs.
func isSafeURL(raw string) error {
	parsedURL, err := url.Parse(raw)
	if err != nil {
		return errors.New("invalid url")
	}
	if parsedURL.Scheme+":" == rdf.BlankNodeReplacement {
		return nil
	}
	if parsedURL.Scheme == "" || parsedURL.Hostname() == "" {
		return errors.New("invalid url")
	}
	ips, err := net.LookupIP(parsedURL.Hostname())
	if err != nil {
		return err
	}

	for _, ip := range ips {
		addr, ok := netip.AddrFromSlice(ip)
		if !ok {
			return errors.New("invalid IP")
		}
		if !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() {
			return errors.New("blocked IP")
		}
	}
	return nil
}
