package api

import (
	"net/http"
	"rdf-store-backend/base"
	"slices"
	"strings"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

type JSONError struct {
	Error string `json:"error"`
}

var Router = gin.New()
var BasePath = "/api/v1"
var livelinessEndpoint = "/healthz"

// init configures CORS and base routes for the API router.
func init() {
	corsConfig := cors.New(cors.Config{
		AllowOrigins:     base.AllowedOrigins,
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Length", "Content-Type"},
		ExposeHeaders:    []string{"Content-Length", "Location"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	})
	// exclude liveliness checks from access logs
	Router.Use(gin.LoggerWithConfig(gin.LoggerConfig{
		SkipPaths: []string{BasePath + livelinessEndpoint},
	}))
	Router.Use(gin.Recovery())
	Router.Use(corsConfig)
	Router.SetTrustedProxies(nil)
	Router.UseRawPath = true
	Router.GET(BasePath+livelinessEndpoint, handleHealthz)
	Router.GET(BasePath+"/config", handleConfig)
}

// handleHealthz returns a lightweight health response for liveness checks.
func handleHealthz(c *gin.Context) {
	c.String(http.StatusOK, "ok")
}

// handleConfig returns runtime configuration and auth context to the client.
func handleConfig(c *gin.Context) {
	writeAccess, user := writeAccessGranted(c.Request.Header)
	config := base.AuthenticatedConfig{
		Config:      base.Configuration,
		User:        user,
		Email:       c.Request.Header.Get(base.AuthEmailHeader),
		WriteAccess: writeAccess,
	}
	c.JSON(http.StatusOK, config)
}

// writeAccessGranted checks headers to determine write access and username.
func writeAccessGranted(h http.Header) (granted bool, user string) {
	if !base.Configuration.AuthEnabled {
		granted = true
		return
	}
	user = h.Get(base.AuthUserHeader)
	if len(user) == 0 {
		return
	}
	if len(base.AuthWriteAccessGroup) > 0 {
		// check if user has required group
		granted = slices.Contains(strings.Split(h.Get(base.AuthGroupsHeader), ","), base.AuthWriteAccessGroup)
	} else {
		granted = true
	}
	return
}
