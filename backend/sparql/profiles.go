package sparql

import (
	"bytes"
	"fmt"
	"rdf-store-backend/base"
	"strconv"

	"github.com/antchfx/xmlquery"
)

var hashPredicate = "<spdx:checksumValue>"

func LoadProfile(id string) (profile []byte, err error) {
	return loadGraph(profileDataset, id)
}

func CreateProfile(id string, profile []byte) error {
	if err := createGraph(profileDataset, id, profile); err != nil {
		return err
	}
	return setHash(id, profile)
}

func UpdateProfile(id string, profile []byte) error {
	if err := deleteHash(id); err != nil {
		return err
	}
	if err := uploadGraph(profileDataset, id, profile); err != nil {
		return err
	}
	return setHash(id, profile)
}

func DeleteProfile(id string) error {
	if err := deleteHash(id); err != nil {
		return err
	}
	return deleteGraph(profileDataset, id)
}

func GetAllProfileIds() ([]string, error) {
	return getAllGraphIds(profileDataset)
}

func GetProfileHash(id string) (uint32, error) {
	resp, err := queryDataset(profileDataset, fmt.Sprintf("SELECT ?hash WHERE { <%s> %s ?hash }", id, hashPredicate))
	if err != nil {
		return 0, err
	}
	doc, err := xmlquery.Parse(bytes.NewReader(resp))
	if err != nil {
		return 0, err
	}
	literal := xmlquery.FindOne(doc, "//literal")
	if literal != nil {
		hash, err := strconv.ParseUint(literal.InnerText(), 10, 32)
		if err != nil {
			return 0, err
		}
		return uint32(hash), nil
	}
	return 0, fmt.Errorf("hash not found for profile %s", id)
}

func setHash(id string, profile []byte) error {
	// set hash
	return updateDataset(profileDataset, fmt.Sprintf("INSERT DATA { <%s> %s %d . }", id, hashPredicate, base.Hash(profile)))
}

func deleteHash(id string) error {
	return updateDataset(profileDataset, fmt.Sprintf(`DELETE WHERE { <%s> %s ?hash . }`, id, hashPredicate))
}
