package main

import (
	"fmt"
	"os"
	"rdf-store-backend/search"
	"rdf-store-backend/sparql"
)

// main runs command-line utilities for administration tasks.
func main() {
	if len(os.Args) < 2 {
		fmt.Println("missing command argument")
		os.Exit(-1)
	}
	switch os.Args[1] {
	case "reindex":
		sparql.ParseAllProfiles()
		search.Reindex()
	default:
		fmt.Println("unknown command", os.Args[1])
		os.Exit(-1)
	}
}
