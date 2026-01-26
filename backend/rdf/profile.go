package rdf

import (
	"bytes"
	"fmt"
	"rdf-store-backend/base"
	"strconv"

	"github.com/deiu/rdf2go"
	"github.com/google/uuid"
	"github.com/knakk/rdf"
	"github.com/knakk/sparql"
)

var hashPredicate = "<spdx:checksumValue>"
var BlankNodeReplacement = "urn:"

// GetProfile loads a profile graph from the profile dataset storage.
// It returns the serialized profile bytes and any error encountered.
func GetProfile(id string) (profile []byte, err error) {
	return loadGraph(profileDataset, id)
}

// UpdateProfile stores a profile after replacing blank nodes and calculating a hash.
// It returns the parsed graph representation alongside any error.
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
	// build hash on original/unmodified profile
	hash := base.Hash(profile)
	// store profile with blank nodes replaced by proper IDs
	if err := uploadGraph(profileDataset, id, buf.Bytes(), graph); err != nil {
		return nil, err
	}
	if err = saveProfileHash(id, hash); err != nil {
		return nil, err
	}
	return graph, nil
}

// DeleteProfile removes a profile graph and its hash metadata from storage.
// It returns an error if either deletion fails.
func DeleteProfile(id string) error {
	if err := deleteProfileHash(id); err != nil {
		return err
	}
	return deleteGraph(profileDataset, id)
}

// GetAllProfileIds lists all profile graph IDs in the dataset.
// It returns the slice of profile IDs and any error encountered.
func GetAllProfileIds() ([]string, error) {
	return getAllGraphIds(profileDataset)
}

// GetProfileHash reads the stored hash for a profile when available.
// It returns a pointer to the hash or nil when missing, plus any error.
func GetProfileHash(id string) (*uint32, error) {
	bindings, err := queryDataset(profileDataset, fmt.Sprintf("SELECT ?hash WHERE { <%s> %s ?hash }", id, hashPredicate))
	if err != nil {
		return nil, err
	}
	res, err := sparql.ParseJSON(bytes.NewReader(bindings))
	if err != nil {
		return nil, err
	}

	if len(res.Results.Bindings) > 0 {
		if hash, okHash := res.Solutions()[0]["hash"].(rdf.Literal); okHash {
			parsed, err := strconv.ParseUint(hash.String(), 10, 32)
			if err != nil {
				return nil, err
			}
			hash := uint32(parsed)
			return &hash, nil
		}
	}
	return nil, nil
}

// saveProfileHash persists the hash value for a profile in the dataset.
// It returns an error if the SPARQL update fails.
func saveProfileHash(id string, hash uint32) error {
	return updateDataset(profileDataset, fmt.Sprintf("INSERT DATA { <%s> %s %d . }", id, hashPredicate, hash))
}

// deleteProfileHash removes the stored hash for a profile from the dataset.
// It returns an error if the SPARQL update fails.
func deleteProfileHash(id string) error {
	return updateDataset(profileDataset, fmt.Sprintf(`DELETE WHERE { <%s> %s ?hash . }`, id, hashPredicate))
}

// replaceBlankNodes substitutes blank nodes with stable resource identifiers for downstream lookups.
// It returns the rewritten graph and any parse error encountered.
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
