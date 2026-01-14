package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path"
	"rdf-store-backend/base"
	"rdf-store-backend/search"
	"rdf-store-backend/shacl"
	"rdf-store-backend/sparql"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/deiu/rdf2go"
	"github.com/robfig/cron/v3"
)

type MPSSearchResultItem struct {
	BaseUrl    string `json:"base_url"`
	Definition string `json:"definition"`
}

var findBaseRegex = regexp.MustCompile(`@base <(.*)>`)
var lock sync.Mutex

func startSyncProfiles() error {
	profiles, err := sparql.ParseAllProfiles()
	if err != nil {
		return err
	}

	if len(base.SyncSchedule) > 0 {
		c := cron.New()
		c.AddFunc(base.SyncSchedule, synchronize)
		c.Start()
		slog.Info("started scheduled profile sync", "cron", base.SyncSchedule, "details", c.Entries())
	}
	// sync immediately if we start with no profiles (empty database) or no schedule
	if len(base.SyncSchedule) == 0 || len(profiles) == 0 {
		synchronize()
	}
	return nil
}

func synchronize() {
	if lock.TryLock() {
		defer lock.Unlock()
		changed, err := synchronizeProfiles()
		if err != nil {
			slog.Error("failed syncing profiles", "error", err)
		} else if changed {
			_, err := sparql.ParseAllProfiles()
			if err != nil {
				slog.Error("failed parsing profiles", "error", err)
			} else {
				search.Reindex()
			}
		}
	} else {
		slog.Warn("Skipping profile synchronization: already running")
	}
}

func synchronizeProfiles() (changed bool, err error) {
	slog.Info("syncing profiles...")
	start := time.Now()

	var profiles []MPSSearchResultItem
	profileIds := make(map[string]bool)

	// load profiles from NFDI4Ing metadata profile service
	if base.MPSEnabled {
		var resp *http.Response
		slog.Debug("loading remote profiles", "endpoint", base.MPSUrl)
		client := http.Client{
			Timeout: 20 * time.Second,
		}
		resp, err = client.Get(base.MPSUrl)
		if err != nil {
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			message := ""
			if body, err := io.ReadAll(resp.Body); err == nil {
				message = string(body)
			}
			err = fmt.Errorf("failed loading remote profiles from %s - status: %v, response: '%v'", base.MPSUrl, resp.StatusCode, message)
			return
		}

		var data []byte
		data, err = io.ReadAll(resp.Body)
		if err != nil {
			return
		}
		if err = json.Unmarshal(data, &profiles); err != nil {
			return
		}
	}
	// load profiles locally
	if base.LocalProfilesEnabled {
		localProfileDir := path.Join("local", "profiles")
		if files, err := os.ReadDir(localProfileDir); err == nil {
			for _, file := range files {
				if !file.IsDir() && strings.HasSuffix(file.Name(), ".ttl") {
					slog.Debug("loading local profile", "file", file.Name())
					if fileContent, err := os.ReadFile(path.Join(localProfileDir, file.Name())); err == nil {
						turtle := string(fileContent)
						base := ""
						if match := findBaseRegex.FindAllStringSubmatch(turtle, 1); len(match) > 0 {
							base = match[0][1]
						}
						if len(base) == 0 {
							slog.Warn("rejecting local profile because it hase no @base definition", "file", file.Name())
						} else {
							profiles = append(profiles, MPSSearchResultItem{
								BaseUrl:    base,
								Definition: turtle,
							})
						}
					}
				}
			}
		} else {
			slog.Error("couldn't read local profiles", "error", err)
		}
	}

	changedOrNewProfiles := make(map[string]*rdf2go.Graph)

	// first pass: store changed or new profiles
	for _, profile := range profiles {
		profileIds[profile.BaseUrl] = true
		// check if profile changed or is new
		profileData := []byte(profile.Definition)
		profileData = base.FixBooleansInRDF(profileData)
		inputHash := base.Hash(profileData)
		existingHash, hashErr := sparql.GetProfileHash(profile.BaseUrl)
		if hashErr != nil || inputHash != existingHash {
			changed = true
			// store profile
			graph, err := sparql.UpdateProfile(profile.BaseUrl, profileData)
			if err != nil {
				return changed, err
			}
			changedOrNewProfiles[profile.BaseUrl] = graph
		}
	}

	// second pass: delete profiles that do not exist anymore
	existingProfileIds, err := sparql.GetAllProfileIds()
	if err != nil {
		slog.Error("failed loading profile IDs", "error", err)
	} else {
		for _, existingProfileId := range existingProfileIds {
			if _, ok := profileIds[existingProfileId]; !ok {
				changed = true
				slog.Info("deleting existing profile", "id", existingProfileId)
				if err := sparql.DeleteProfile(existingProfileId); err != nil {
					slog.Error("failed deleting existing profile", "id", existingProfileId, "error", err)
				}
			}
		}
	}

	// third pass: extract labels from owl:imports
	for _, graph := range changedOrNewProfiles {
		// if err = sparql.UpdateIndexProfile(profileId); err != nil {
		// 	return
		// }
		extractLabelsFromOwlImports(graph, profileIds)
	}
	slog.Info("syncing profiles finished", "profiles", len(profiles), "changed", changed, "duration", time.Since(start))
	return
}

func extractLabelsFromOwlImports(graph *rdf2go.Graph, profileIds map[string]bool) {
	for _, importsStatement := range graph.All(nil, shacl.OWL_IMPORTS, nil) {
		url := importsStatement.Object.RawValue()
		// ignore owl:imports that reference profiles
		if _, ok := profileIds[url]; !ok {
			// load owl:imports only once
			if exist, err := sparql.CheckLabelsExist(url); err == nil && !exist {
				slog.Debug("loading owl:imports", "url", url)
				graph, err := sparql.ImportLabelsFromUrl(url)
				if err != nil {
					slog.Debug("failed loading owl:imports", "url", url, "error", err)
				} else {
					// recurse
					extractLabelsFromOwlImports(graph, profileIds)
				}
			}
		}
	}
}
