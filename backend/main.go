package main

import (
	"embed"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"path/filepath"
	"rdf-store-backend/api"
	"rdf-store-backend/search"
	"strings"

	"github.com/gin-gonic/gin"
)

func main() {
	// handle non-API requests by trying to serve embedded static files (frontend and swagger UI)
	api.Router.NoRoute(serveStaticFiles())
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

//go:embed frontend/*
var frontend embed.FS

//go:embed swagger/*
var swagger embed.FS

func serveStaticFiles() func(c *gin.Context) {
	frontendFS, err := fs.Sub(frontend, "frontend")
	if err != nil {
		log.Fatal(err)
	}

	swaggerFS, err := fs.Sub(swagger, "swagger")
	if err != nil {
		log.Fatal(err)
	}

	return func(c *gin.Context) {
		// fallback routes for frontend (everything not starting with API base path)
		if !strings.HasPrefix(c.Request.RequestURI, api.BasePath) {
			filename := strings.TrimPrefix(c.Request.RequestURI, "/")
			file, err := frontendFS.Open(filename)
			headers := make(map[string]string)
			if err != nil {
				// serve index.html to enable client side routing
				file, err = frontendFS.Open("index.html")
				if err != nil {
					c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
					return
				}
				headers["Cache-Control"] = "no-cache"
			}
			defer file.Close()
			stat, _ := file.Stat()
			c.DataFromReader(http.StatusOK, stat.Size(), mime.TypeByExtension(filepath.Ext(filename)), file, headers)
		} else {
			// fallback routes for swagger UI
			filename := strings.TrimPrefix(strings.TrimPrefix(c.Request.RequestURI, api.BasePath), "/")
			if filename == "" {
				filename = "index.html"
			}
			file, err := swaggerFS.Open(filename)
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
				return
			}
			defer file.Close()
			stat, _ := file.Stat()
			c.DataFromReader(http.StatusOK, stat.Size(), mime.TypeByExtension(filepath.Ext(filename)), file, nil)
		}
	}
}
