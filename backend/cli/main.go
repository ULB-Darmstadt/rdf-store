package main

import (
	"fmt"
	"os"
	"rdf-store-backend/profilesync"
	"rdf-store-backend/rdf"
	"rdf-store-backend/search"
)

// main runs command-line utilities for administration tasks.
func main() {
	if len(os.Args) < 2 {
		fmt.Println("missing command argument")
		os.Exit(-1)
	}
	switch os.Args[1] {
	case "reindex":
		rdf.ParseAllProfiles()
		search.Reindex()
	case "rebuild":
		rdf.ParseAllProfiles()
		rebuildResourceMeta()
	case "sync":
		profilesync.Synchronize()
	default:
		fmt.Println("unknown command", os.Args[1])
		os.Exit(-1)
	}
}

func rebuildResourceMeta() {
	resourceIds, err := rdf.GetAllResourceIds()
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, resourceId := range resourceIds {
		fmt.Println("update resource meta for", resourceId)
		_, _, err := rdf.UpdateResourceMetadata(resourceId)
		if err != nil {
			fmt.Println(err)
		}
	}
}
