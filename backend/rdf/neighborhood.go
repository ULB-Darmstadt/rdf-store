package rdf

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"

	"github.com/knakk/rdf"
	"github.com/knakk/sparql"
)

const (
	NeighborhoodOutgoing = "outgoing"
	NeighborhoodIncoming = "incoming"
)

var ErrInvalidNeighborhoodRequest = errors.New("invalid graph neighborhood request")

// GetGraphNeighborhood returns direct RDF statements for one entity or for a
// page of entities that link to it. Incoming results contain only the matching
// relationship statements; clients can explicitly request each source's
// outgoing neighborhood afterward.
func GetGraphNeighborhood(subject, direction string, offset, limit int) ([]byte, int, error) {
	query, countQuery, err := buildNeighborhoodQueries(subject, direction, offset, limit)
	if err != nil {
		return nil, 0, err
	}

	bindings, err := queryDataset(ResourceDataset, query)
	if err != nil {
		return nil, 0, err
	}
	quads, err := sparqlResultToNQuads(bindings)
	if err != nil {
		return nil, 0, err
	}
	if countQuery == "" {
		return quads, 0, nil
	}

	countBindings, err := queryDataset(ResourceDataset, countQuery)
	if err != nil {
		return nil, 0, err
	}
	total, err := parseNeighborhoodCount(countBindings)
	if err != nil {
		return nil, 0, err
	}
	return quads, total, nil
}

func buildNeighborhoodQueries(subject, direction string, offset, limit int) (string, string, error) {
	if !isValidIRI(subject) {
		return "", "", fmt.Errorf("%w: invalid subject IRI: %v", ErrInvalidNeighborhoodRequest, subject)
	}
	if offset < 0 {
		return "", "", fmt.Errorf("%w: offset must not be negative", ErrInvalidNeighborhoodRequest)
	}
	if limit < 1 || limit > 100 {
		return "", "", fmt.Errorf("%w: limit must be between 1 and 100", ErrInvalidNeighborhoodRequest)
	}

	switch direction {
	case NeighborhoodOutgoing:
		return fmt.Sprintf(`SELECT ?s ?p ?o ?g WHERE {
  GRAPH ?g { BIND(<%s> AS ?s) ?s ?p ?o }
} ORDER BY ?g ?p ?o`, subject), "", nil
	case NeighborhoodIncoming:
		query := fmt.Sprintf(`SELECT (?source AS ?s) (?linkPredicate AS ?p) (<%s> AS ?o) ?g WHERE {
  {
    SELECT DISTINCT ?source WHERE {
      GRAPH ?linkGraph { ?source ?linkPredicate <%s> }
      FILTER(isIRI(?source))
    }
    ORDER BY ?source
    LIMIT %d
    OFFSET %d
  }
  GRAPH ?g { ?source ?linkPredicate <%s> }
} ORDER BY ?s ?g ?p`, subject, subject, limit, offset, subject)
		countQuery := fmt.Sprintf(`SELECT (COUNT(DISTINCT ?source) AS ?total) WHERE {
  GRAPH ?g { ?source ?predicate <%s> }
  FILTER(isIRI(?source))
}`, subject)
		return query, countQuery, nil
	default:
		return "", "", fmt.Errorf("%w: direction must be %q or %q", ErrInvalidNeighborhoodRequest, NeighborhoodOutgoing, NeighborhoodIncoming)
	}
}

func parseNeighborhoodCount(bindings []byte) (int, error) {
	result, err := sparql.ParseJSON(bytes.NewReader(bindings))
	if err != nil {
		return 0, err
	}
	if len(result.Solutions()) != 1 {
		return 0, fmt.Errorf("invalid neighborhood count response")
	}
	literal, ok := result.Solutions()[0]["total"].(rdf.Literal)
	if !ok {
		return 0, fmt.Errorf("invalid neighborhood count binding")
	}
	total, err := strconv.Atoi(literal.String())
	if err != nil {
		return 0, fmt.Errorf("invalid neighborhood count: %w", err)
	}
	return total, nil
}
