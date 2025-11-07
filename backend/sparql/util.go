package sparql

import (
	"bytes"
	"errors"
	"rdf-store-backend/base"
	"rdf-store-backend/shacl"

	"github.com/deiu/rdf2go"
)

var Profiles map[string]*shacl.NodeShape

func ParseAllProfiles() (map[string]*shacl.NodeShape, error) {
	profileIds, err := GetAllProfileIds()
	if err != nil {
		return nil, err
	}
	Profiles = make(map[string]*shacl.NodeShape)
	for _, profileId := range profileIds {
		profile, err := LoadProfile(profileId)
		if err != nil {
			return nil, err
		}
		graph, err := base.ParseGraph(bytes.NewReader(profile))
		if err != nil {
			return nil, err
		}
		Profiles[profileId] = new(shacl.NodeShape).Parse(rdf2go.NewResource(profileId), graph)
	}
	// recursively merge overridden properties into parent to prevent duplicate facets
	// for _, profile := range profiles {
	// 	mergeOverriddenPropertiesIntoParent(profile, profiles)
	// }
	base.Configuration.Profiles = profileIds
	return Profiles, nil
}

func FindResourceProfile(graph *rdf2go.Graph, id *rdf2go.Term) (resourceID rdf2go.Term, profile *shacl.NodeShape, err error) {
	var refs []*rdf2go.Triple
	if id == nil {
		refs = graph.All(nil, shacl.DCTERMS_CONFORMS_TO, nil)
		refs = append(refs, graph.All(nil, shacl.RDF_TYPE, nil)...)
	} else {
		refs = graph.All(*id, shacl.DCTERMS_CONFORMS_TO, nil)
		refs = append(refs, graph.All(*id, shacl.RDF_TYPE, nil)...)
	}
	for _, triple := range refs {
		if profileRef, ok := Profiles[triple.Object.RawValue()]; ok {
			if resourceID != nil {
				return nil, nil, errors.New("graph has multiple relations " + shacl.DCTERMS_CONFORMS_TO.String() + " or " + shacl.RDF_TYPE.String() + " to a known SHACL profile")
			}
			resourceID = triple.Subject
			profile = profileRef
		}
	}
	if resourceID == nil {
		return nil, nil, errors.New("graph has no relation " + shacl.DCTERMS_CONFORMS_TO.String() + " or " + shacl.RDF_TYPE.String() + " to a known SHACL profile")
	}
	return
}

// func mergeOverriddenPropertiesIntoParent(profile *shacl.NodeShape, profiles map[string]*shacl.NodeShape) {
// 	for parent := range profile.Parents {
// 		if parentProfile, ok := profiles[parent.RawValue()]; ok {
// 			for _, ownProperty := range profile.Properties {
// 				for _, parentProperty := range parentProfile.Properties {
// 					if ownProperty.Path.Equal(parentProperty.Path) {
// 						parentProperty.Merge(ownProperty)
// 						ownProperty.Ignore = true
// 					}
// 				}
// 			}
// 			mergeOverriddenPropertiesIntoParent(parentProfile, profiles)
// 		} else {
// 			slog.Error("parent profile not found", "id", parent.RawValue())
// 		}
// 	}
// }
