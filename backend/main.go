package main

import (
	"embed"
	"log"
	"net/http"
	"rdf-store-backend/api"
	"rdf-store-backend/search"
	"strings"

	"github.com/gin-contrib/static"
	"github.com/gin-gonic/gin"
)

//go:embed frontend/*
var frontend embed.FS

//go:embed swagger/*
var swagger embed.FS

func main() {
	frontendFS, err := static.EmbedFolder(frontend, "frontend")
	if err != nil {
		log.Fatal(err)
	}
	api.Router.Use(static.Serve("/", frontendFS))

	swaggerFS, err := static.EmbedFolder(swagger, "swagger")
	if err != nil {
		log.Fatal(err)
	}
	api.Router.Use(static.Serve(api.BasePath, swaggerFS))

	api.Router.NoRoute(func(c *gin.Context) {
		// only serve index.html for non-API routes
		if !strings.HasPrefix(c.Request.RequestURI, "/api") {
			index, err := frontendFS.Open("index.html")
			if err != nil {
				log.Fatal(err)
			}
			defer index.Close()
			stat, _ := index.Stat()
			c.Header("Cache-Control", "no-cache")
			http.ServeContent(c.Writer, c.Request, "index.html", stat.ModTime(), index)
		}
	})
	go func() {
		if err := search.Init(false); err != nil {
			log.Fatal(err)
		}
		if err := startSyncProfiles(); err != nil {
			log.Fatal(err)
		}
	}()
	if err := api.Router.Run(":3000"); err != nil {
		log.Fatal(err)
	}
}
