package main

import (
	"log/slog"
	"rdf-store-backend/base"
	"rdf-store-backend/profilesync"
	"rdf-store-backend/sparql"

	"github.com/robfig/cron/v3"
)

// startSyncProfiles loads profiles and starts optional scheduled sync.
func startSyncProfiles() error {
	profiles, err := sparql.ParseAllProfiles()
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
