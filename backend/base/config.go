package base

import (
	"fmt"
	"log/slog"
	"regexp"
	"strings"
)

type Config struct {
	Layout              string   `json:"layout"`
	Profiles            []string `json:"profiles"`
	Index               string   `json:"index"`
	GeoDataType         string   `json:"geoDataType"`
	SolrMaxAggregations int      `json:"solrMaxAggregations"`
	AuthEnabled         bool     `json:"authEnabled"`
	ContactEmail        string   `json:"contactEmail,omitempty"`
	RdfNamespace        string   `json:"rdfNamespace"`
}

type AuthenticatedConfig struct {
	Config
	User        string `json:"authUser,omitempty"`
	Email       string `json:"authEmail,omitempty"`
	WriteAccess bool   `json:"authWriteAccess"`
}

var Configuration = Config{
	Layout:              EnvVar("LAYOUT", "default"),
	Profiles:            make([]string, 0),
	Index:               SolrIndex,
	GeoDataType:         "http://www.opengis.net/ont/geosparql#wktLiteral",
	SolrMaxAggregations: 1000,
	AuthEnabled:         len(EnvVar("DISABLE_OAUTH", "dummy")) == 0,
	ContactEmail:        EnvVar("CONTACT_EMAIL", ""),
	RdfNamespace:        EnvVar("RDF_NAMESPACE", "http://example.org/"),
}

var ExposeFusekiFrontend = EnvVarAsBool("EXPOSE_FUSEKI_FRONTEND", false)
var LocalProfilesEnabled = EnvVarAsBool("LOCAL_PROFILES_ENABLED", true)
var MPSEnabled = EnvVarAsBool("MPS_ENABLED", false)
var MPSQuery = EnvVar("MPS_QUERY", "")
var MPSLanguage = EnvVar("MPS_LANGUAGE", "EN")
var MPSCommunity = EnvVar("MPS_COMMUNITY", "")
var MPSEndpoint = EnvVar("MPS_ENDPOINT", "https://pg4aims.ulb.tu-darmstadt.de/AIMS/application-profiles")
var MPSUrl = fmt.Sprintf("%s/?query=%s&language=%s&community=%s&includeDefinition=true", MPSEndpoint, MPSQuery, MPSLanguage, MPSCommunity)
var SolrIndex = EnvVar("SOLR_INDEX", "rdf")
var ValidatorEndpoint = EnvVar("VALIDATOR_ENDPOINT", "http://localhost:8000")
var RdfStandardTaxonomies = EnvVar("RDF_STANDARD_TAXONOMIES", "")

// var SyncSchedule = EnvVar("CRON", "*/5 * * * *") // every 5 minutes
var SyncSchedule = EnvVar("CRON", "")

var solrRegex = `[\/*?"<>|#:.\- ]`
var solrRegexReplacement = "_"
var solrStringReplacer = regexp.MustCompile(solrRegex)
var logLevel = EnvVar("LOG_LEVEL", "INFO")

func init() {
	// set log level
	var level slog.Level
	if err := level.UnmarshalText([]byte(logLevel)); err == nil {
		slog.SetLogLoggerLevel(level)
	}
}

func CleanStringForSolr(s string) string {
	return solrStringReplacer.ReplaceAllString(strings.ToLower(s), solrRegexReplacement)
}
