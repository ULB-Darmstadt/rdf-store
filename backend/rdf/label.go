package rdf

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/http"
	"rdf-store-backend/base"
	"rdf-store-backend/shacl"
	"strings"
	"text/template"

	"github.com/deiu/rdf2go"
	"github.com/knakk/rdf"
	"github.com/knakk/sparql"
	"golang.org/x/exp/slices"
)

const fallbackLanguage = "en"

var LabelPredicates = map[string]bool{
	shacl.SHACL_NAME.RawValue():      true,
	shacl.SKOS_PREF_LABEL.RawValue(): true,
	shacl.RDFS_LABEL.RawValue():      true,
	shacl.DCTERMS_TITLE.RawValue():   true,
	shacl.FOAF_NAME.RawValue():       true,
}
var labelTargetPredicate = shacl.RDFS_LABEL.String()
var labelsQuery = `
SELECT DISTINCT ?id ?label
WHERE {
  GRAPH ?g {
	VALUES ?id { {{range .Ids}}{{.}} {{end}} }
	?id ?p ?label .
  }
}
`
var labelsQueryTemplate = template.Must(template.New("listQuery").Funcs(template.FuncMap{}).Parse(labelsQuery))

// GetLabels retrieves preferred labels for IDs in the given language.
// It returns a map of ID to label and any error encountered.
func GetLabels(language string, ids []string) (map[string]string, error) {
	var result = make(map[string]string)
	if len(ids) > 0 {
		languagePrios := []string{language}
		if len(language) > 2 {
			languagePrios = append(languagePrios, language[:2])
		}
		if language != fallbackLanguage {
			languagePrios = append(languagePrios, fallbackLanguage)
		}
		languagePrios = append(languagePrios, "")

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

		currentLabelPrios := make(map[string]int)
		for _, row := range res.Solutions() {
			s, okS := row["id"].(rdf.Subject)
			label, okO := row["label"].(rdf.Literal)
			if !okS || !okO {
				return nil, fmt.Errorf("invalid binding: %v", row)
			}
			id := "<" + s.String() + ">"
			labelLang := label.Lang()
			labelPrio := slices.Index(languagePrios, labelLang)
			if labelPrio > -1 {
				currentPrio, ok := currentLabelPrios[id]
				if !ok || labelPrio < currentPrio {
					result[id] = label.String()
					currentLabelPrios[id] = labelPrio
				}
			}
		}
	}
	return result, nil
}

// CheckLabelsExist checks whether labels for a URL were already imported.
// It returns a boolean flag and any error from the dataset lookup.
func CheckLabelsExist(url string) (bool, error) {
	return checkGraphExists(labelDataset, url)
}

// ExtractLabels stores label triples and optional SHACL-derived labels.
// It returns an error if label extraction or upload fails.
func ExtractLabels(id string, graph *rdf2go.Graph, convertShaclProperties bool) error {
	var result bytes.Buffer
	var profileLables map[string]string
	if convertShaclProperties {
		profileLables = findProfileLabels(rdf2go.NewResource(id), graph)
	}
	for triple := range graph.IterTriples() {
		if _, isLabel := LabelPredicates[triple.Predicate.RawValue()]; isLabel {
			// check if triple object is a literal
			if label, ok := triple.Object.(*rdf2go.Literal); ok {
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
							lang = "en"
						}
						if graph.One(triple.Subject, shacl.SHACL_QUALIFIED_VALUE_SHAPE, nil) != nil || graph.One(triple.Subject, shacl.SHACL_NODE, nil) != nil {
							if profileLabel, ok := profileLables[lang]; ok {
								prefixedLabel = profileLabel + " > " + prefixedLabel
							}
						}
						fmt.Fprintf(&result, "<:%s> %s %s .\n", base.CleanStringForSolr(triple.Subject.RawValue()), labelTargetPredicate, rdf2go.NewLiteralWithLanguage(prefixedLabel, lang).String())
					}
				}
				fmt.Fprintf(&result, "%s %s %s .\n", triple.Subject.String(), labelTargetPredicate, triple.Object.String())
			}
		}
	}

	if result.Len() > 0 {
		return uploadGraph(labelDataset, id, result.Bytes(), nil)
	}
	return nil
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
	labels := make(map[string]string)
	for labelPredicate := range LabelPredicates {
		for _, labelTriple := range graph.All(id, rdf2go.NewResource(labelPredicate), nil) {
			if spec, ok := labelTriple.Object.(*rdf2go.Literal); ok {
				lang := spec.Language
				if len(lang) == 0 {
					lang = "en"
				}
				if _, ok := labels[lang]; !ok {
					labels[lang] = spec.Value
				}
			}
		}
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
