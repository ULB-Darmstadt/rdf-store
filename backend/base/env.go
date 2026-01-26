package base

import (
	"log"
	"os"
	"strconv"
	"strings"
)

var BackendUrl = EnvVar("BACKEND_URL", "http://localhost:3000")
var AllowedOrigins = append([]string{BackendUrl}, EnvVarAsStringSlice("ALLOWED_ORIGINS")...)
var AuthUserHeader = "X-User"
var AuthEmailHeader = "X-Email"
var AuthGroupsHeader = "X-Groups"
var AuthWriteAccessGroup = EnvVar("WRITE_ACCESS_GROUP", "")

// EnvVar reads an environment variable and falls back to a default when unset.
// It returns the resolved string value.
func EnvVar(key string, defaultValue string) string {
	if val, present := os.LookupEnv(key); present {
		return val
	}
	return defaultValue
}

// EnvVarAsInt parses an environment variable into an integer with a fallback for invalid values.
// It returns the parsed integer or the default value when parsing fails.
func EnvVarAsInt(key string, defaultValue int) int {
	if val, present := os.LookupEnv(key); present {
		res, err := strconv.Atoi(val)
		if err != nil {
			log.Printf("warning: env var '%s' with value '%s' is not an integer. using default: %d\n", key, val, defaultValue)
			return defaultValue
		} else {
			return res
		}
	}
	return defaultValue
}

// EnvVarAsBool parses an environment variable into a boolean with a fallback for invalid values.
// It returns the parsed boolean or the default value when parsing fails.
func EnvVarAsBool(key string, defaultValue bool) bool {
	if val, present := os.LookupEnv(key); present {
		res, err := strconv.ParseBool(val)
		if err != nil {
			log.Printf("warning: env var '%s' with value '%s' is not a boolean. using default: %v\n", key, val, defaultValue)
			return defaultValue
		} else {
			return res
		}
	}
	return defaultValue
}

// EnvVarAsStringSlice splits a comma-separated environment variable into trimmed values.
// It returns the non-empty entries in order, or an empty slice when unset.
func EnvVarAsStringSlice(key string) []string {
	var result []string
	if val, present := os.LookupEnv(key); present {
		for _, v := range strings.Split(val, ",") {
			value := strings.TrimSpace(v)
			if value != "" {
				result = append(result, value)
			}
		}
	}
	return result
}
