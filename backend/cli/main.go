package main

import (
	"bytes"
	"fmt"
	"os"
	"rdf-store-backend/base"
	"rdf-store-backend/profilesync"
	"rdf-store-backend/rdf"
	"rdf-store-backend/search"
)

var commands = []string{"reindex", "rebuild", "sync", "relabel"}

func init() {
	if _, err := rdf.ParseAllProfiles(); err != nil {
		panic(err)
	}
}

// main runs command-line utilities for administration tasks.
func main() {
	if len(os.Args) < 2 {
		fmt.Println("missing command argument")
		os.Exit(-1)
	}
	switch os.Args[1] {
	case commands[0]:
		search.Reindex()
	case commands[1]:
		rebuildResourceMeta()
		search.Reindex()
	case commands[2]:
		profilesync.Synchronize()
	case commands[3]:
		reextractLabels()
	default:
		fmt.Println("unknown command", os.Args[1], "known commands:", commands)
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

func reextractLabels() {
	profileIds, err := rdf.GetAllProfileIds()
	if err != nil {
		panic(err)
	}
	for _, profileId := range profileIds {
		data, err := rdf.GetProfile(profileId)
		if err != nil {
			panic(err)
		}
		graph, err := base.ParseGraph(bytes.NewReader(data))
		if err != nil {
			panic(err)
		}
		fmt.Println("extract labels of profile", profileId)
		if err := rdf.ExtractLabels(profileId, graph, true); err != nil {
			panic(err)
		}
	}
	resourceIds, err := rdf.GetAllResourceIds()
	if err != nil {
		panic(err)
	}
	for _, resourceId := range resourceIds {
		data, _, err := rdf.GetResource(resourceId, false)
		if err != nil {
			panic(err)
		}
		graph, err := base.ParseGraph(bytes.NewReader(data))
		if err != nil {
			panic(err)
		}
		fmt.Println("extract labels of resource", resourceId)
		if err := rdf.ExtractLabels(resourceId, graph, false); err != nil {
			panic(err)
		}
	}
}
