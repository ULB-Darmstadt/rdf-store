package rdf

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/knakk/rdf"
	"github.com/knakk/sparql"
)

const (
	NeighborhoodOutgoing = "outgoing"
	NeighborhoodIncoming = "incoming"
)

var ErrInvalidNeighborhoodRequest = errors.New("invalid graph neighborhood request")

const MaxLocalSubjectCandidates = 100

type GraphNeighborhood struct {
	Quads         string   `json:"quads"`
	LocalSubjects []string `json:"localSubjects"`
	Offset        int      `json:"offset"`
	Limit         int      `json:"limit"`
	Returned      int      `json:"returned"`
	HasMore       bool     `json:"hasMore"`
	NextOffset    int      `json:"nextOffset"`
}

// GetLocalSubjects returns the candidates that can be resolved from the local
// resource dataset, either as a described subject or as a stored named graph.
func GetLocalSubjects(ids []string) ([]string, error) {
	query, err := buildLocalSubjectsQuery(ids)
	if err != nil || query == "" {
		return []string{}, err
	}
	bindings, err := queryDataset(ResourceDataset, query)
	if err != nil {
		return nil, err
	}
	return parseLocalSubjects(bindings)
}

func buildLocalSubjectsQuery(ids []string) (string, error) {
	if len(ids) > MaxLocalSubjectCandidates {
		return "", fmt.Errorf("%w: at most %d ids are allowed", ErrInvalidNeighborhoodRequest, MaxLocalSubjectCandidates)
	}
	unique := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if !isValidIRI(id) || strings.ContainsAny(id, "<>\"{}|^`\\ \t\r\n") {
			return "", fmt.Errorf("%w: invalid id IRI: %v", ErrInvalidNeighborhoodRequest, id)
		}
		unique[id] = struct{}{}
	}
	if len(unique) == 0 {
		return "", nil
	}
	values := make([]string, 0, len(unique))
	for id := range unique {
		values = append(values, id)
	}
	sort.Strings(values)
	return fmt.Sprintf(`SELECT DISTINCT ?id WHERE {
  VALUES ?id { %s }
  { GRAPH ?g { ?id ?p ?o } }
  UNION
  { GRAPH ?id { ?s ?gp ?go } }
} ORDER BY ?id`, arrayToSparqlValues(values)), nil
}

func parseLocalSubjects(bindings []byte) ([]string, error) {
	result, err := sparql.ParseJSON(bytes.NewReader(bindings))
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(result.Solutions()))
	for _, row := range result.Solutions() {
		id, ok := row["id"].(rdf.IRI)
		if !ok {
			return nil, fmt.Errorf("invalid local subject binding: %v", row)
		}
		ids = append(ids, id.String())
	}
	return ids, nil
}

// GetGraphNeighborhood returns one deterministic page of direct RDF
// statements and identifies the named subjects in that page that are locally
// available for further traversal.
func GetGraphNeighborhood(subject, direction string, offset, limit int) (*GraphNeighborhood, error) {
	query, err := buildNeighborhoodQuery(subject, direction, offset, limit)
	if err != nil {
		return nil, err
	}

	bindings, err := queryDataset(ResourceDataset, query)
	if err != nil {
		return nil, err
	}
	quads, pageSubjects, adjacent, returned, hasMore, err := parseNeighborhoodPage(bindings, limit)
	if err != nil {
		return nil, err
	}
	localSubjects, err := GetLocalSubjects(adjacent)
	if err != nil {
		return nil, err
	}
	// Every named subject returned by the neighborhood query is known to occur
	// in the local dataset. Include those without another lookup.
	localSubjects = append(localSubjects, pageSubjects...)
	sort.Strings(localSubjects)
	localSubjects = compactStrings(localSubjects)
	return &GraphNeighborhood{
		Quads:         string(quads),
		LocalSubjects: localSubjects,
		Offset:        offset,
		Limit:         limit,
		Returned:      returned,
		HasMore:       hasMore,
		NextOffset:    offset + returned,
	}, nil
}

func compactStrings(values []string) []string {
	if len(values) < 2 {
		return values
	}
	result := values[:1]
	for _, value := range values[1:] {
		if value != result[len(result)-1] {
			result = append(result, value)
		}
	}
	return result
}

func buildNeighborhoodQuery(subject, direction string, offset, limit int) (string, error) {
	if !isValidIRI(subject) {
		return "", fmt.Errorf("%w: invalid subject IRI: %v", ErrInvalidNeighborhoodRequest, subject)
	}
	if offset < 0 {
		return "", fmt.Errorf("%w: offset must not be negative", ErrInvalidNeighborhoodRequest)
	}
	if limit < 1 || limit > 100 {
		return "", fmt.Errorf("%w: limit must be between 1 and 100", ErrInvalidNeighborhoodRequest)
	}

	switch direction {
	case NeighborhoodOutgoing:
		return fmt.Sprintf(`SELECT ?s ?p ?o ?g ?pageP ?pageO WHERE {
  {
    SELECT ?pageP ?pageO ?g WHERE {
      GRAPH ?g { <%s> ?pageP ?pageO }
    }
    ORDER BY ?g ?pageP ?pageO
    LIMIT %d
    OFFSET %d
  }
  GRAPH ?g {
    {
      <%s> ?pageP ?pageO
    }
    UNION
    {
      FILTER(isBlank(?pageO))
      ?pageO <http://www.w3.org/1999/02/22-rdf-syntax-ns#rest>* ?cellS .
      FILTER(isBlank(?cellS))
      ?cellS ?cellP ?cellO
    }
  }
  BIND(COALESCE(?cellS, <%s>) AS ?s)
  BIND(COALESCE(?cellP, ?pageP) AS ?p)
  BIND(COALESCE(?cellO, ?pageO) AS ?o)
} ORDER BY ?g ?pageP ?pageO ?s ?p ?o`, subject, limit+1, offset, subject, subject), nil
	case NeighborhoodIncoming:
		return fmt.Sprintf(`SELECT ?s ?p (<%s> AS ?o) ?g WHERE {
  GRAPH ?g { ?s ?p <%s> }
  FILTER(isIRI(?s))
} ORDER BY ?g ?s ?p ?o
LIMIT %d
OFFSET %d`, subject, subject, limit+1, offset), nil
	default:
		return "", fmt.Errorf("%w: direction must be %q or %q", ErrInvalidNeighborhoodRequest, NeighborhoodOutgoing, NeighborhoodIncoming)
	}
}

func parseNeighborhoodPage(bindings []byte, limit int) ([]byte, []string, []string, int, bool, error) {
	result, err := sparql.ParseJSON(bytes.NewReader(bindings))
	if err != nil {
		return nil, nil, nil, 0, false, err
	}
	solutions := result.Solutions()
	returned := len(solutions)
	hasMore := returned > limit
	if len(solutions) > 0 && solutions[0]["pageP"] != nil && solutions[0]["pageO"] != nil {
		pages := make([][]map[string]rdf.Term, 0)
		pageKeys := make(map[string]int)
		for _, row := range solutions {
			key := fmt.Sprintf("%T:%v\x00%T:%v\x00%T:%v", row["g"], row["g"], row["pageP"], row["pageP"], row["pageO"], row["pageO"])
			page, ok := pageKeys[key]
			if !ok {
				page = len(pages)
				pageKeys[key] = page
				pages = append(pages, nil)
			}
			pages[page] = append(pages[page], row)
		}
		returned = min(len(pages), limit)
		hasMore = len(pages) > limit
		solutions = nil
		for _, page := range pages[:returned] {
			solutions = append(solutions, page...)
		}
	} else if hasMore {
		solutions = solutions[:limit]
		returned = len(solutions)
	}
	var encoded bytes.Buffer
	encoder := rdf.NewQuadEncoder(&encoded, rdf.NQuads)
	pageSubjects := make(map[string]struct{})
	adjacent := make(map[string]struct{})
	for _, row := range solutions {
		s, okS := row["s"].(rdf.Subject)
		p, okP := row["p"].(rdf.Predicate)
		o, okO := row["o"].(rdf.Object)
		g, okG := row["g"].(rdf.Context)
		if !okS || !okP || !okO || !okG {
			return nil, nil, nil, 0, false, fmt.Errorf("invalid quad: %v", row)
		}
		if iri, ok := s.(rdf.IRI); ok {
			pageSubjects[iri.String()] = struct{}{}
		}
		if iri, ok := o.(rdf.IRI); ok {
			adjacent[iri.String()] = struct{}{}
		}
		if err := encoder.Encode(rdf.Quad{Triple: rdf.Triple{Subj: s, Pred: p, Obj: o}, Ctx: g}); err != nil {
			return nil, nil, nil, 0, false, err
		}
	}
	if err := encoder.Close(); err != nil {
		return nil, nil, nil, 0, false, err
	}
	known := make([]string, 0, len(pageSubjects))
	for id := range pageSubjects {
		known = append(known, id)
	}
	sort.Strings(known)
	candidates := make([]string, 0, len(adjacent))
	for id := range adjacent {
		candidates = append(candidates, id)
	}
	sort.Strings(candidates)
	return encoded.Bytes(), known, candidates, returned, hasMore, nil
}
