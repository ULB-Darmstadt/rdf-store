package main

import (
	"embed"
	"io/fs"
	"log"
	"log/slog"
	"mime"
	"net/http"
	"path/filepath"
	"rdf-store-backend/api"
	"rdf-store-backend/base"
	"rdf-store-backend/profilesync"
	"rdf-store-backend/rdf"
	"rdf-store-backend/search"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/robfig/cron/v3"
)

// main starts background tasks and serves the HTTP API plus static files.
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

// startSyncProfiles loads profiles and starts the optional scheduled sync loop.
// It returns an error when profile parsing fails or scheduling cannot be set up.
func startSyncProfiles() error {
	profiles, err := rdf.ParseAllProfiles()
	if err != nil {
		return err
	}

	if len(base.SyncSchedule) > 0 {
		c := cron.New()
		c.AddFunc(base.SyncSchedule, profilesync.Synchronize)
		c.Start()
		slog.Info("started scheduled profile sync", "cron", base.SyncSchedule, "details", c.Entries())
	}
	// sync immediately if we start with no profiles (empty database) or no schedule
	if len(base.SyncSchedule) == 0 || len(profiles) == 0 {
		profilesync.Synchronize()
	}
	return nil
}

//go:embed frontend/*
var frontend embed.FS

//go:embed swagger/*
var swagger embed.FS

// serveStaticFiles builds a handler for embedded frontend and swagger UI assets.
// It returns a gin handler function that serves the static files or JSON errors.
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
				filename = "index.html"
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
