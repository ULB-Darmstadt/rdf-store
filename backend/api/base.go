package api

import (
	"net/http"
	"rdf-store-backend/base"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

type JSONError struct {
	Error string `json:"error"`
}

var Router = gin.Default()
var BasePath = "/api/v1"

func init() {
	corsConfig := cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Length", "Content-Type"},
		ExposeHeaders:    []string{"Content-Length", "Location"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	})
	Router.Use(corsConfig)
	Router.SetTrustedProxies(nil)
	Router.UseRawPath = true
	Router.GET(BasePath+"/config", handleConfig)
}

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
