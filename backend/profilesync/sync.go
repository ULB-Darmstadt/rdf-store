package profilesync

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path"
	"rdf-store-backend/base"
	"rdf-store-backend/rdf"
	"rdf-store-backend/search"
	"rdf-store-backend/shacl"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/deiu/rdf2go"
)

type MPSSearchResultItem struct {
	BaseUrl    string `json:"base_url"`
	Definition string `json:"definition"`
}

var findBaseRegex = regexp.MustCompile(`@base <(.*)>`)
var lock sync.Mutex

// Synchronize runs profile sync and triggers reindexing when changes are detected.
func Synchronize() {
	if lock.TryLock() {
		defer lock.Unlock()
		changedOrDeletedProfiles, err := synchronizeProfiles()
		if err != nil {
			slog.Error("failed syncing profiles", "error", err)
		} else if len(changedOrDeletedProfiles) > 0 {
			_, err := rdf.ParseAllProfiles()
			if err != nil {
				slog.Error("failed parsing profiles", "error", err)
			} else {
				for _, profileId := range changedOrDeletedProfiles {
					resourcesToUpdate, err := rdf.FindConformingResources(profileId)
					if err != nil {
						slog.Error("failed getting conforming resources for changed profile", "id", profileId, "error", err)
					} else {
						for _, resourceId := range resourcesToUpdate {
							slog.Debug("updating metadata and search index for resource", "id", resourceId)
							metadata, graph, err := rdf.UpdateResourceMetadata(resourceId)
							if err != nil {
								slog.Error("failed updating resource metadata", "id", resourceId, "error", err)
							} else {
								if err := search.IndexResource(graph, metadata); err != nil {
									slog.Error("failed updating search index for resource", "id", resourceId, "error", err)
								}
							}
						}
					}
				}
			}
		}
	} else {
		slog.Warn("Skipping profile synchronization: already running")
	}
}

// synchronizeProfiles fetches profiles from sources and updates datasets.
// It returns IDs of changed or deleted profiles along with any error encountered.
func synchronizeProfiles() (changedOrDeletedProfiles []string, err error) {
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

	changedProfiles := make(map[string]*rdf2go.Graph)
	newProfiles := make(map[string]*rdf2go.Graph)
	deletedProfiles := make(map[string]bool)

	// first pass: store changed or new profiles
	for _, profile := range profiles {
		profileIds[profile.BaseUrl] = true
		// check if profile changed or is new
		profileData := []byte(profile.Definition)
		profileData = base.FixBooleansInRDF(profileData)
		inputHash := base.Hash(profileData)
		existingHash, hashErr := rdf.GetProfileHash(profile.BaseUrl)
		if hashErr != nil {
			slog.Warn("failed retrieving hash for profile", "id", profile.BaseUrl)
		} else {
			if existingHash == nil {
				// no hash -> new profile, so store it
				graph, err := rdf.UpdateProfile(profile.BaseUrl, profileData)
				if err != nil {
					return nil, err
				}
				newProfiles[profile.BaseUrl] = graph
			} else if inputHash != *existingHash {
				// hash changed -> profile changed, so update it
				graph, err := rdf.UpdateProfile(profile.BaseUrl, profileData)
				if err != nil {
					return nil, err
				}
				changedProfiles[profile.BaseUrl] = graph
			}
		}
	}

	// second pass: delete profiles that do not exist anymore
	existingProfileIds, err := rdf.GetAllProfileIds()
	if err != nil {
		slog.Error("failed loading profile IDs", "error", err)
	} else {
		for _, existingProfileId := range existingProfileIds {
			if _, ok := profileIds[existingProfileId]; !ok {
				slog.Info("deleting existing profile", "id", existingProfileId)
				if err := rdf.DeleteProfile(existingProfileId); err != nil {
					slog.Error("failed deleting existing profile", "id", existingProfileId, "error", err)
				} else {
					deletedProfiles[existingProfileId] = true
				}
			}
		}
	}

	// third pass: extract labels from owl:imports of changed or new profiles
	for _, graph := range changedProfiles {
		extractLabelsFromOwlImports(graph, profileIds)
	}
	for _, graph := range newProfiles {
		extractLabelsFromOwlImports(graph, profileIds)
	}
	changedOrDeletedProfiles = make([]string, 0)
	for id := range changedProfiles {
		changedOrDeletedProfiles = append(changedOrDeletedProfiles, id)
	}
	for id := range deletedProfiles {
		changedOrDeletedProfiles = append(changedOrDeletedProfiles, id)
	}

	slog.Info("syncing profiles finished", "profiles", len(profiles), "#new", len(newProfiles), "#changed", len(changedProfiles), "#deleted", len(deletedProfiles), "duration", time.Since(start))
	return
}

// extractLabelsFromOwlImports recursively imports labels from owl:imports.
func extractLabelsFromOwlImports(graph *rdf2go.Graph, profileIds map[string]bool) {
	for _, importsStatement := range graph.All(nil, shacl.OWL_IMPORTS, nil) {
		url := importsStatement.Object.RawValue()
		// ignore owl:imports that reference profiles
		if _, ok := profileIds[url]; !ok {
			// load owl:imports only once
			if exist, err := rdf.CheckLabelsExist(url); err == nil && !exist {
				slog.Debug("loading owl:imports", "url", url)
				graph, err := rdf.ImportLabelsFromUrl(url)
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
