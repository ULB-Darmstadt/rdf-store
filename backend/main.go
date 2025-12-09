package main

import (
	"embed"
	"log"
	"rdf-store-backend/api"
	"rdf-store-backend/search"

	"github.com/gin-contrib/static"
)

//go:embed frontend/*
var frontend embed.FS

func main() {
	fs, err := static.EmbedFolder(frontend, "frontend")
	if err != nil {
		log.Fatal(err)
	}
	api.Router.Use(static.Serve("/", fs))

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
