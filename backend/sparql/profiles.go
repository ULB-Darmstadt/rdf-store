package sparql

import (
	"bytes"
	"fmt"
	"rdf-store-backend/base"
	"strconv"

	"github.com/antchfx/xmlquery"
	"github.com/deiu/rdf2go"
	"github.com/google/uuid"
)

var hashPredicate = "<spdx:checksumValue>"
var BlankNodeReplacement = "rdf-store:"

func LoadProfile(id string) (profile []byte, err error) {
	return loadGraph(profileDataset, id)
}

func UpdateProfile(id string, profile []byte) (*rdf2go.Graph, error) {
	graph, err := replaceBlankNodes(profile)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err = graph.Serialize(&buf, "text/turtle"); err != nil {
		return nil, err
	}
	if err := deleteProfileHash(id); err != nil {
		return nil, err
	}
	// build hash before modifying profile
	hash := base.Hash(profile)
	// store profile with blank nodes replaced by proper IDs
	if err := uploadGraph(profileDataset, id, buf.Bytes(), graph); err != nil {
		return nil, err
	}
	if err = setProfileHash(id, hash); err != nil {
		return nil, err
	}
	return graph, nil
}

func DeleteProfile(id string) error {
	if err := deleteProfileHash(id); err != nil {
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

func setProfileHash(id string, hash uint32) error {
	return updateDataset(profileDataset, fmt.Sprintf("INSERT DATA { <%s> %s %d . }", id, hashPredicate, hash))
}

func deleteProfileHash(id string) error {
	return updateDataset(profileDataset, fmt.Sprintf(`DELETE WHERE { <%s> %s ?hash . }`, id, hashPredicate))
}

/*
We need to convert blank nodes to proper named nodes so that they can be referred to (e.g. by the search facets or for validating against specific qualifiedValueShapes).
*/
func replaceBlankNodes(profile []byte) (graph *rdf2go.Graph, err error) {
	input, err := base.ParseGraph(bytes.NewReader(profile))
	if err != nil {
		return
	}
	graph = rdf2go.NewGraph("")
	mappings := make(map[string]rdf2go.Term) // blank node id -> new named node
	for t := range input.IterTriples() {
		// dereferencing copies the triple
		output := *t
		if spec, ok := output.Subject.(*rdf2go.BlankNode); ok {
			if replacement, ok := mappings[spec.RawValue()]; ok {
				output.Subject = replacement
			} else {
				replacement := rdf2go.NewResource(BlankNodeReplacement + uuid.NewString())
				mappings[spec.RawValue()] = replacement
				output.Subject = replacement
			}
		}
		if spec, ok := output.Object.(*rdf2go.BlankNode); ok {
			if replacement, ok := mappings[spec.RawValue()]; ok {
				output.Object = replacement
			} else {
				replacement := rdf2go.NewResource(BlankNodeReplacement + uuid.NewString())
				mappings[spec.RawValue()] = replacement
				output.Object = replacement
			}
		}
		graph.Add(&output)
	}
	return
}
