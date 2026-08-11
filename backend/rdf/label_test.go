package rdf

import (
	"bytes"
	"strings"
	"testing"

	"github.com/deiu/rdf2go"
)

func TestWriteCombinedPersonLabelsCombinesLastNameAndFirstName(t *testing.T) {
	graph := rdf2go.NewGraph("")
	data := `
@prefix foaf: <http://xmlns.com/foaf/0.1/> .
@prefix ex: <http://example.org/> .
ex:leonard foaf:firstName "Leonard"@en ;
	foaf:lastName "Nimoy"@en .
ex:noSurname foaf:firstName "Only"@en .
ex:localized foaf:firstName "Jean-Luc"@fr ;
	foaf:lastName "Picard"@en .
ex:unsupported foaf:firstName "Jean-Luc"@fr ;
	foaf:lastName "Picard"@fr .
`
	if err := graph.Parse(strings.NewReader(data), "text/turtle"); err != nil {
		t.Fatal(err)
	}
	var result bytes.Buffer
	writeCombinedPersonLabels(graph, &result)
	output := result.String()
	if !strings.Contains(output, `<http://example.org/leonard> <http://www.w3.org/2000/01/rdf-schema#label> "Nimoy, Leonard"@en`) {
		t.Errorf("expected combined label, got:\n%s", output)
	}
	if strings.Contains(output, "noSurname") {
		t.Errorf("must not combine a first name without a last name:\n%s", output)
	}
	if strings.Contains(output, "Picard, Jean-Luc") {
		t.Errorf("must not combine names from different or unsupported languages:\n%s", output)
	}
}

func TestWriteCombinedPersonLabelsSkipsNonLiteralNames(t *testing.T) {
	graph := rdf2go.NewGraph("")
	data := `
@prefix foaf: <http://xmlns.com/foaf/0.1/> .
@prefix ex: <http://example.org/> .
ex:resolved foaf:firstName ex:someNode ;
	foaf:lastName ex:someOtherNode .
`
	if err := graph.Parse(strings.NewReader(data), "text/turtle"); err != nil {
		t.Fatal(err)
	}
	var result bytes.Buffer
	writeCombinedPersonLabels(graph, &result)
	if result.Len() != 0 {
		t.Errorf("expected no combined label for non-literal names, got:\n%s", result.String())
	}
}

func TestWriteCombinedPersonLabelsDoesNotOverrideFullLabel(t *testing.T) {
	graph := rdf2go.NewGraph("")
	data := `
@prefix foaf: <http://xmlns.com/foaf/0.1/> .
@prefix skos: <http://www.w3.org/2004/02/skos/core#> .
@prefix ex: <http://example.org/> .
ex:person foaf:firstName "Leonard"@en ;
	foaf:lastName "Nimoy"@en ;
	skos:prefLabel "Leonard Nimoy"@en .
`
	if err := graph.Parse(strings.NewReader(data), "text/turtle"); err != nil {
		t.Fatal(err)
	}
	var result bytes.Buffer
	writeCombinedPersonLabels(graph, &result)
	if result.Len() != 0 {
		t.Errorf("expected no generated label when a full label exists, got:\n%s", result.String())
	}
}

func TestLabelCandidatePrioritizesLanguageBeforePredicate(t *testing.T) {
	languages := preferredLanguagePriorities("de-DE")
	germanLabel := labelCandidate{
		value:             "Bezeichnung",
		languagePriority:  languages["de"],
		predicatePriority: LabelPredicates["http://www.w3.org/2000/01/rdf-schema#label"],
	}
	englishPreferredLabel := labelCandidate{
		value:             "Preferred label",
		languagePriority:  languages["en"],
		predicatePriority: LabelPredicates["http://www.w3.org/2004/02/skos/core#prefLabel"],
	}
	if !germanLabel.precedes(englishPreferredLabel) {
		t.Fatal("a requested-language label must precede a higher-ranked fallback-language predicate")
	}
}

func TestPreferredLanguagePrioritiesIgnoreUnsupportedRequest(t *testing.T) {
	priorities := preferredLanguagePriorities("fr")
	if _, exists := priorities["fr"]; exists {
		t.Fatal("unsupported requested language must not be considered")
	}
	if priorities["en"] >= priorities["de"] {
		t.Fatalf("configured fallback order must prefer en over de: %#v", priorities)
	}
	if _, exists := priorities[""]; !exists {
		t.Fatal("untagged labels must remain available as a final fallback")
	}
}

func TestLabelCandidatePrioritizesPredicateWithinLanguage(t *testing.T) {
	preferred := labelCandidate{value: "Preferred", predicatePriority: LabelPredicates["http://www.w3.org/2004/02/skos/core#prefLabel"]}
	generic := labelCandidate{value: "Generic", predicatePriority: LabelPredicates["http://www.w3.org/2000/01/rdf-schema#label"]}
	if !preferred.precedes(generic) {
		t.Fatal("skos:prefLabel must precede rdfs:label in the same language")
	}
}

func TestFindProfileLabelsUsesPredicatePriority(t *testing.T) {
	graph := rdf2go.NewGraph("")
	data := `
@prefix sh: <http://www.w3.org/ns/shacl#> .
@prefix rdfs: <http://www.w3.org/2000/01/rdf-schema#> .
@prefix ex: <http://example.org/> .
ex:property rdfs:label "Global label"@en ;
	sh:name "Context name"@en .
`
	if err := graph.Parse(strings.NewReader(data), "text/turtle"); err != nil {
		t.Fatal(err)
	}
	labels := findProfileLabels(rdf2go.NewResource("http://example.org/property"), graph)
	if labels["en"] != "Context name" {
		t.Fatalf("expected sh:name to win for a property shape, got %q", labels["en"])
	}
}

func TestSerializeLabelsPreservesSourcePredicates(t *testing.T) {
	graph := rdf2go.NewGraph("")
	data := `
@prefix skos: <http://www.w3.org/2004/02/skos/core#> .
@prefix rdfs: <http://www.w3.org/2000/01/rdf-schema#> .
@prefix ex: <http://example.org/> .
ex:resource skos:prefLabel "Preferred"@en ;
	rdfs:label "Generic"@en, "Allgemein"@de, "Ignoré"@fr .
`
	if err := graph.Parse(strings.NewReader(data), "text/turtle"); err != nil {
		t.Fatal(err)
	}
	output := string(serializeLabels("http://example.org/graph", graph, false))
	if !strings.Contains(output, `<http://example.org/resource> <http://www.w3.org/2004/02/skos/core#prefLabel> "Preferred"@en`) {
		t.Errorf("expected the SKOS predicate to be preserved, got:\n%s", output)
	}
	if !strings.Contains(output, `<http://example.org/resource> <http://www.w3.org/2000/01/rdf-schema#label> "Generic"@en`) {
		t.Errorf("expected the RDFS predicate to be preserved, got:\n%s", output)
	}
	if !strings.Contains(output, `<http://example.org/resource> <http://www.w3.org/2000/01/rdf-schema#label> "Allgemein"@de`) {
		t.Errorf("expected the supported German label to be preserved, got:\n%s", output)
	}
	if strings.Contains(output, "Ignoré") {
		t.Errorf("expected the unsupported French label to be ignored, got:\n%s", output)
	}
}
