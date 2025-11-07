package sparql

import (
	"bytes"
	"fmt"
	"rdf-store-backend/base"
	"rdf-store-backend/shacl"
	"regexp"
	"text/template"

	"github.com/antchfx/xmlquery"
	"github.com/deiu/rdf2go"
	"golang.org/x/exp/slices"
)

const fallbackLanguage = "en"

var labelPredicates = map[string]bool{
	shacl.SHACL_NAME.String():      true,
	shacl.SKOS_PREF_LABEL.String(): true,
	shacl.RDFS_LABEL.String():      true,
	shacl.DCTERMS_TITLE.String():   true,
	shacl.FOAF_NAME.String():       true,
}
var labelTargetPredicate = shacl.RDFS_LABEL.String()
var resourceRegex = regexp.MustCompile("<.*>")
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

		labelsTmplInput := map[string]interface{}{"Ids": ids}
		var query bytes.Buffer
		if err := labelsQueryTemplate.Execute(&query, labelsTmplInput); err != nil {
			return nil, err
		}
		queryResult, err := queryDataset(labelDataset, query.String())
		if err != nil {
			return nil, err
		}
		doc, err := xmlquery.Parse(bytes.NewReader(queryResult))
		if err != nil {
			return nil, err
		}
		currentLabelPrios := make(map[string]int)
		xmlquery.FindEach(doc, "//uri", func(ctr int, node *xmlquery.Node) {
			id := "<" + node.InnerText() + ">"
			label := xmlquery.FindOne(node.Parent.Parent, "binding[@name='label']/literal")
			labelLang := label.SelectAttr("xml:lang")
			labelPrio := slices.Index(languagePrios, labelLang)
			if labelPrio > -1 {
				currentPrio, ok := currentLabelPrios[id]
				if !ok || labelPrio < currentPrio {
					result[id] = label.InnerText()
					currentLabelPrios[id] = labelPrio
				}
			}
		})
	}
	return result, nil
}

func ExtractLabels(id string, graph *rdf2go.Graph, convertShaclProperties bool) error {
	var result bytes.Buffer

	for triple := range graph.IterTriples() {
		if _, isLabel := labelPredicates[triple.Predicate.String()]; isLabel {
			sub := triple.Subject.String()
			subjectIsResource := resourceRegex.MatchString(sub)
			if convertShaclProperties {
				// check if this is a label for a shacl node shape
				if node := graph.One(triple.Subject, shacl.RDF_TYPE, shacl.SHACL_NODE_SHAPE); node != nil {
					// addtionally add converted id to labels graph (for the facet titles)
					fmt.Fprintf(&result, "<:%s> %s %s .\n", base.CleanStringForSolr(triple.Subject.RawValue()), labelTargetPredicate, triple.Object.String())
				} else if path := graph.One(triple.Subject, shacl.SHACL_PATH, nil); path != nil {
					// id is a shacl property (we just assume that this is the case if sh:path exists)
					sub = fmt.Sprintf("<:%s.%s>", base.CleanStringForSolr(id), base.CleanStringForSolr(path.Object.RawValue()))
					// shacl properties might be blank nodes, but we converted their id, so pretend that they are resources
					subjectIsResource = true
				}
			}
			if subjectIsResource {
				fmt.Fprintf(&result, "%s %s %s .\n", sub, labelTargetPredicate, triple.Object.String())
			}
		}
	}

	if result.Len() > 0 {
		return uploadGraph(labelDataset, id, result.Bytes())
	}
	return nil
}
