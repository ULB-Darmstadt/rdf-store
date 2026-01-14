package sparql

import (
	"net/url"
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
	// first pass: parse profiles
	for _, profileId := range profileIds {
		profile, err := GetProfile(profileId)
		if err != nil {
			return nil, err
		}
		parsed, err := new(shacl.NodeShape).Parse(rdf2go.NewResource(profileId), &profile)
		if err != nil {
			return nil, err
		}
		Profiles[profileId] = parsed
		// register sub profiles (i.e. node shapes previously converted from blank nodes)
		for _, nodeShapeTriple := range parsed.Graph.All(nil, shacl.RDF_TYPE, shacl.SHACL_NODE_SHAPE) {
			if nodeShapeTriple.Subject.RawValue() != profileId {
				parsedSubProfile, err := new(shacl.NodeShape).Parse(nodeShapeTriple.Subject, &profile)
				if err != nil {
					return nil, err
				}
				Profiles[nodeShapeTriple.Subject.RawValue()] = parsedSubProfile
			}
		}
	}

	// denormalizedProfiles := make(map[*shacl.NodeShape]bool)
	for _, profile := range Profiles {
		// profile.DenormalizeQualifiedValueShapes(Profiles)
		profile.DenormalizePropertyNodeShapes(Profiles)
	}

	// for _, profile := range Profiles {
	// 	profile.Print()
	// }

	profileIds = make([]string, 0)
	for id := range Profiles {
		profileIds = append(profileIds, id)
	}
	base.Configuration.Profiles = profileIds
	return Profiles, nil
}

func isValidIRI(value string) bool {
	u, err := url.Parse(value)
	return err == nil && u.Scheme != ""
}
