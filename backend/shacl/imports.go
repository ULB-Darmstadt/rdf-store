package shacl

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/deiu/rdf2go"
)

// SerializeProfileClosure combines a root profile with all locally available
// profiles referenced through owl:imports. Keeping this resolution local makes
// validation independent of whether imported profile IRIs are HTTP-accessible.
func SerializeProfileClosure(root *NodeShape, profiles map[string]*NodeShape) ([]byte, error) {
	if root == nil || root.Graph == nil {
		return nil, fmt.Errorf("missing root profile graph")
	}

	merged := rdf2go.NewGraph("")
	visited := make(map[*rdf2go.Graph]bool)
	nextBlankNode := 0

	var addProfile func(*NodeShape)
	addProfile = func(profile *NodeShape) {
		if profile == nil || profile.Graph == nil || visited[profile.Graph] {
			return
		}
		visited[profile.Graph] = true
		blankNodes := make(map[string]rdf2go.Term)
		scopeBlankNode := func(term rdf2go.Term) rdf2go.Term {
			blank, ok := term.(*rdf2go.BlankNode)
			if !ok {
				return term
			}
			if scoped := blankNodes[blank.RawValue()]; scoped != nil {
				return scoped
			}
			scoped := rdf2go.NewResource(fmt.Sprintf("urn:rdf-store:profile-closure:%d", nextBlankNode))
			nextBlankNode++
			blankNodes[blank.RawValue()] = scoped
			return scoped
		}

		for triple := range profile.Graph.IterTriples() {
			// A locally resolved import is already copied into the merged graph.
			// Omitting its directive prevents the validator from fetching and
			// adding a duplicate remote copy. Keep unresolved imports as fallback.
			if triple.Predicate.Equal(OWL_IMPORTS) && findProfile(profiles, triple.Object.RawValue()) != nil {
				continue
			}
			// rdf2go can expand Turtle's `()` into a synthetic blank node that
			// only has rdf:rest rdf:nil. References to that node are normalized
			// below, so the malformed synthetic list node itself must be omitted.
			if _, blank := triple.Subject.(*rdf2go.BlankNode); blank &&
				triple.Predicate.Equal(RDF_LIST_REST) &&
				profile.Graph.One(triple.Subject, RDF_LIST_FIRST, nil) == nil {
				continue
			}
			copy := *triple
			copy.Subject = scopeBlankNode(copy.Subject)
			// rdf2go represents Turtle's empty list `()` as an orphan blank
			// node when parsing some serialized Fuseki graphs. Once merged and
			// renamed it is no longer serialized as `()`, so normalize the tail
			// explicitly to rdf:nil.
			if _, blank := copy.Object.(*rdf2go.BlankNode); blank &&
				copy.Predicate.Equal(RDF_LIST_REST) &&
				profile.Graph.One(copy.Object, RDF_LIST_FIRST, nil) == nil {
				copy.Object = RDF_LIST_NIL
			} else {
				copy.Object = scopeBlankNode(copy.Object)
			}
			merged.Add(&copy)
		}

		for _, triple := range profile.Graph.All(nil, OWL_IMPORTS, nil) {
			if imported := findProfile(profiles, triple.Object.RawValue()); imported != nil {
				addProfile(imported)
			}
		}
	}

	addProfile(root)
	var output bytes.Buffer
	if err := merged.Serialize(&output, "text/turtle"); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

// findProfile tolerates the common trailing-slash difference between an
// owl:imports object and the named graph identifier used in local storage.
func findProfile(profiles map[string]*NodeShape, id string) *NodeShape {
	if profile := profiles[id]; profile != nil {
		return profile
	}
	if strings.HasSuffix(id, "/") {
		return profiles[strings.TrimSuffix(id, "/")]
	}
	return profiles[id+"/"]
}
