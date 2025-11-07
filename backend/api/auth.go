package api

import (
	"net/http"
	"rdf-store-backend/base"
	"slices"
	"strings"
)

func writeAccessGranted(h http.Header) (granted bool, user string) {
	if !base.Configuration.AuthEnabled {
		granted = true
		return
	}
	user = h.Get(base.AuthUserHeader)
	if len(user) == 0 {
		return
	}
	if len(base.AuthWriteAccessGroup) > 0 {
		// check if user has required group
		granted = slices.Index(strings.Split(h.Get(base.AuthGroupsHeader), ","), base.AuthWriteAccessGroup) > -1
	} else {
		granted = true
	}
	return
}
