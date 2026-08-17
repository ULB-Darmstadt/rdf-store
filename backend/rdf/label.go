package rdf

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/http"
	"rdf-store-backend/base"
	"rdf-store-backend/shacl"
	"sort"
	"strings"
	"text/template"

	"github.com/deiu/rdf2go"
	"github.com/knakk/rdf"
	"github.com/knakk/sparql"
)

// LabelPredicates ranks supported RDF naming properties from most to least
// suitable as a display label. SKOS explicitly identifies prefLabel as the
// preferred lexical label. SHACL says tools should prefer a property's sh:name
// over global labels. Full names and titles rank ahead of FOAF name components.
var LabelPredicates = map[string]int{
	shacl.SKOS_PREF_LABEL.RawValue(): 0,
	shacl.SHACL_NAME.RawValue():      1,
	shacl.RDFS_LABEL.RawValue():      2,
	shacl.DCTERMS_TITLE.RawValue():   3,
	shacl.FOAF_NAME.RawValue():       4,
	shacl.FOAF_LAST_NAME.RawValue():  5,
	shacl.FOAF_FIRST_NAME.RawValue(): 6,
	shacl.SCHEMA_TITLE.RawValue():    7,
	shacl.SCHEMA_HEADLINE.RawValue(): 8,
}
var labelTargetPredicate = shacl.RDFS_LABEL.String()
var supportedLabelLanguages = languageSet(base.LabelLanguages)
var labelsQuery = `
SELECT DISTINCT ?id ?p ?label
WHERE {
  GRAPH ?g {
	VALUES ?id { {{range .Ids}}{{.}} {{end}} }
	?id ?p ?label .
  }
}
`
var labelsQueryTemplate = template.Must(template.New("listQuery").Funcs(template.FuncMap{}).Parse(labelsQuery))

// FindLabels collects literal labels for a subject in predicate-priority order.
// The deterministic order is also significant for composite display labels:
// FOAF lastName precedes firstName, so consumers render "lastName, firstName".
func FindLabels(subject rdf2go.Term, graph *rdf2go.Graph) []string {
	type candidate struct {
		value    string
		priority int
	}
	var candidates []candidate
	for _, triple := range graph.All(subject, nil, nil) {
		priority, ok := LabelPredicates[triple.Predicate.RawValue()]
		if !ok {
			continue
		}
		if literal, ok := triple.Object.(*rdf2go.Literal); ok {
			candidates = append(candidates, candidate{value: literal.RawValue(), priority: priority})
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].priority != candidates[j].priority {
			return candidates[i].priority < candidates[j].priority
		}
		return candidates[i].value < candidates[j].value
	})
	labels := make([]string, len(candidates))
	for i, candidate := range candidates {
		labels[i] = candidate.value
	}
	return labels
}

// GetLabels retrieves preferred labels for IDs in the given language.
// It returns a map of ID to label and any error encountered.
func GetLabels(language string, ids []string) (map[string]string, error) {
	result := make(map[string]string)
	if len(ids) > 0 {
		languagePriorities := preferredLanguagePriorities(language)

		labelsTmplInput := map[string]any{"Ids": ids}
		var query bytes.Buffer
		if err := labelsQueryTemplate.Execute(&query, labelsTmplInput); err != nil {
			return nil, err
		}

		bindings, err := queryDataset(labelDataset, query.String())
		if err != nil {
			return nil, err
		}
		res, err := sparql.ParseJSON(bytes.NewReader(bindings))
		if err != nil {
			return nil, err
		}

		selected := make(map[string]labelCandidate)
		for _, row := range res.Solutions() {
			s, okS := row["id"].(rdf.Subject)
			predicate, okP := row["p"].(rdf.IRI)
			label, okO := row["label"].(rdf.Literal)
			if !okS || !okP || !okO {
				return nil, fmt.Errorf("invalid binding: %v", row)
			}
			predicatePriority, accepted := LabelPredicates[predicate.String()]
			if !accepted {
				continue
			}
			languagePriority, accepted := languagePriorities[strings.ToLower(label.Lang())]
			if !accepted {
				continue
			}
			id := "<" + s.String() + ">"
			candidate := labelCandidate{value: label.String(), languagePriority: languagePriority, predicatePriority: predicatePriority}
			if current, ok := selected[id]; !ok || candidate.precedes(current) {
				selected[id] = candidate
			}
		}
		for id, candidate := range selected {
			result[id] = candidate.value
		}
	}
	return result, nil
}

// GetDefaultLabels retrieves preferred labels using the configured primary
// label language and the standard fallback order.
func GetDefaultLabels(ids []string) (map[string]string, error) {
	return GetLabels(defaultLabelLanguage(), ids)
}

type labelCandidate struct {
	value             string
	languagePriority  int
	predicatePriority int
}

func (candidate labelCandidate) precedes(other labelCandidate) bool {
	if candidate.languagePriority != other.languagePriority {
		return candidate.languagePriority < other.languagePriority
	}
	if candidate.predicatePriority != other.predicatePriority {
		return candidate.predicatePriority < other.predicatePriority
	}
	// SPARQL result order is unspecified, so make malformed duplicate labels
	// deterministic without assigning meaning to their arrival order.
	return candidate.value < other.value
}

func preferredLanguagePriorities(language string) map[string]int {
	priorities := make(map[string]int)
	add := func(candidate string) {
		candidate = normalizeLanguage(candidate)
		if candidate != "" && !isSupportedLabelLanguage(candidate) {
			return
		}
		if _, exists := priorities[candidate]; !exists {
			priorities[candidate] = len(priorities)
		}
	}
	language = strings.TrimSpace(language)
	add(language)
	if baseLanguage, _, found := strings.Cut(language, "-"); found {
		add(baseLanguage)
	}
	for _, supportedLanguage := range base.LabelLanguages {
		add(supportedLanguage)
	}
	add("")
	return priorities
}

func languageSet(languages []string) map[string]bool {
	result := make(map[string]bool, len(languages))
	for _, language := range languages {
		if normalized := normalizeLanguage(language); normalized != "" {
			result[normalized] = true
		}
	}
	return result
}

func normalizeLanguage(language string) string {
	return strings.ToLower(strings.TrimSpace(language))
}

func isSupportedLabelLanguage(language string) bool {
	language = normalizeLanguage(language)
	if language == "" {
		return true
	}
	return supportedLabelLanguages[language]
}

func defaultLabelLanguage() string {
	for _, language := range base.LabelLanguages {
		if normalized := normalizeLanguage(language); normalized != "" {
			return normalized
		}
	}
	return ""
}

// CheckLabelsExist checks whether labels for a URL were already imported.
// It returns a boolean flag and any error from the dataset lookup.
func CheckLabelsExist(url string) (bool, error) {
	return checkGraphExists(labelDataset, url)
}

// ExtractLabels stores label triples and optional SHACL-derived labels.
// It returns an error if label extraction or upload fails.
func ExtractLabels(id string, graph *rdf2go.Graph, convertShaclProperties bool) error {
	labels := serializeLabels(id, graph, convertShaclProperties)
	if len(labels) == 0 {
		return nil
	}
	return uploadGraph(labelDataset, id, labels, nil)
}

func serializeLabels(id string, graph *rdf2go.Graph, convertShaclProperties bool) []byte {
	var result bytes.Buffer
	var profileLabels map[string]string
	if convertShaclProperties {
		profileLabels = findProfileLabels(rdf2go.NewResource(id), graph)
	}
	for triple := range graph.IterTriples() {
		if _, isLabel := LabelPredicates[triple.Predicate.RawValue()]; isLabel {
			// check if triple object is a literal
			if label, ok := triple.Object.(*rdf2go.Literal); ok {
				if !isSupportedLabelLanguage(label.Language) {
					continue
				}
				if convertShaclProperties {
					// check if this is a label for a shacl node shape
					if node := graph.One(triple.Subject, shacl.RDF_TYPE, shacl.SHACL_NODE_SHAPE); node != nil {
						// addtionally add converted id to labels graph (for the facet titles)
						fmt.Fprintf(&result, "<:%s> %s %s .\n", base.CleanStringForSolr(triple.Subject.RawValue()), labelTargetPredicate, label.String())
					} else if path := graph.One(triple.Subject, shacl.SHACL_PATH, nil); path != nil {
						// addtionally add converted id to labels graph (for the facet titles)
						prefixedLabel := label.Value
						lang := label.Language
						if len(lang) == 0 {
							lang = defaultLabelLanguage()
						}
						if prefixQualifiedPropertyLabels && (graph.One(triple.Subject, shacl.SHACL_QUALIFIED_VALUE_SHAPE, nil) != nil || graph.One(triple.Subject, shacl.SHACL_NODE, nil) != nil) {
							if profileLabel, ok := profileLabels[lang]; ok {
								prefixedLabel = profileLabel + " > " + prefixedLabel
							}
						}
						fmt.Fprintf(&result, "<:%s> %s %s .\n", base.CleanStringForSolr(triple.Subject.RawValue()), labelTargetPredicate, rdf2go.NewLiteralWithLanguage(prefixedLabel, lang).String())
					}
				}
				// Keep the source predicate: GetLabels needs it to distinguish an
				// explicitly preferred label from a generic label or name component.
				fmt.Fprintf(&result, "%s %s %s .\n", triple.Subject.String(), triple.Predicate.String(), triple.Object.String())
			}
		}
	}
	// Add a display label for people that only provide legacy FOAF name parts.
	writeCombinedPersonLabels(graph, &result)

	return result.Bytes()
}

// writeCombinedPersonLabels emits an rdfs:label "lastName, firstName" when a
// subject has matching FOAF name parts but no full label in that language. A
// generated value must not be asserted as skos:prefLabel because that property
// records the vocabulary publisher's preferred lexical label.
func writeCombinedPersonLabels(graph *rdf2go.Graph, result *bytes.Buffer) {
	firstNames := make(map[string]map[string]string) // subject IRI -> language -> value
	lastNames := make(map[string]map[string]string)
	fullLabelLanguages := make(map[string]map[string]bool)
	for triple := range graph.IterTriples() {
		var subjectNames map[string]map[string]string
		switch triple.Predicate.RawValue() {
		case shacl.FOAF_FIRST_NAME.RawValue():
			subjectNames = firstNames
		case shacl.FOAF_LAST_NAME.RawValue():
			subjectNames = lastNames
		default:
			if priority, accepted := LabelPredicates[triple.Predicate.RawValue()]; accepted && priority < LabelPredicates[shacl.FOAF_LAST_NAME.RawValue()] {
				if literal, ok := triple.Object.(*rdf2go.Literal); ok {
					if !isSupportedLabelLanguage(literal.Language) {
						continue
					}
					subject := triple.Subject.RawValue()
					if fullLabelLanguages[subject] == nil {
						fullLabelLanguages[subject] = make(map[string]bool)
					}
					fullLabelLanguages[subject][strings.ToLower(literal.Language)] = true
				}
			}
			continue
		}
		literal, ok := triple.Object.(*rdf2go.Literal)
		if !ok {
			continue
		}
		if !isSupportedLabelLanguage(literal.Language) {
			continue
		}
		subject := triple.Subject.RawValue()
		if subjectNames[subject] == nil {
			subjectNames[subject] = make(map[string]string)
		}
		subjectNames[subject][strings.ToLower(literal.Language)] = literal.Value
	}
	for subject, firsts := range firstNames {
		lasts, ok := lastNames[subject]
		if !ok {
			continue
		}
		for lang, first := range firsts {
			if fullLabelLanguages[subject][strings.ToLower(lang)] {
				continue
			}
			last, ok := lasts[lang]
			if !ok {
				continue
			}
			label := rdf2go.NewLiteralWithLanguage(last+", "+first, lang)
			fmt.Fprintf(result, "%s %s %s .\n", rdf2go.NewResource(subject).String(), shacl.RDFS_LABEL.String(), label.String())
		}
	}
}

// ImportLabelsFromUrl loads an RDF graph from a URL and extracts labels.
// It returns the parsed graph and any error encountered.
func ImportLabelsFromUrl(url string) (*rdf2go.Graph, error) {
	slog.Info("importing labels from", "url", url)
	header := http.Header{}
	header["Accept"] = []string{"text/turtle"}
	data, err := base.CacheLoad(url, "text/turtle")
	if err != nil {
		return nil, err
	}
	graph, err := base.ParseGraph(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if err = ExtractLabels(url, graph, false); err != nil {
		return nil, err
	}
	return graph, nil
}

// findProfileLabels gathers label literals for a profile resource.
// It returns a map of language to label string.
func findProfileLabels(id rdf2go.Term, graph *rdf2go.Graph) map[string]string {
	candidates := make(map[string]labelCandidate)
	for labelPredicate, predicatePriority := range LabelPredicates {
		for _, labelTriple := range graph.All(id, rdf2go.NewResource(labelPredicate), nil) {
			if spec, ok := labelTriple.Object.(*rdf2go.Literal); ok {
				if !isSupportedLabelLanguage(spec.Language) {
					continue
				}
				lang := strings.ToLower(spec.Language)
				if len(lang) == 0 {
					lang = defaultLabelLanguage()
				}
				candidate := labelCandidate{value: spec.Value, predicatePriority: predicatePriority}
				if current, exists := candidates[lang]; !exists || candidate.precedes(current) {
					candidates[lang] = candidate
				}
			}
		}
	}
	labels := make(map[string]string, len(candidates))
	for language, candidate := range candidates {
		labels[language] = candidate.value
	}
	return labels
}

// importLabelsFromStandardTaxonomies loads labels from configured taxonomies.
// It returns an error if any taxonomy import fails.
func importLabelsFromStandardTaxonomies() error {
	for _, url := range base.RdfStandardTaxonomies {
		url = strings.TrimSpace(url)
		if url != "" {
			if exist, err := CheckLabelsExist(url); err == nil && !exist {
				if _, err := ImportLabelsFromUrl(url); err != nil {
					slog.Warn("failed importing labels from standard taxonomy", "url", url)
				}
			}
		}
	}
	return nil
}
