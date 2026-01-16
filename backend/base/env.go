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

// EnvVar returns an environment variable or a default value.
func EnvVar(key string, defaultValue string) string {
	if val, present := os.LookupEnv(key); present {
		return val
	}
	return defaultValue
}

// EnvVarAsInt parses an environment variable into an integer with fallback.
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

// EnvVarAsBool parses an environment variable into a boolean with fallback.
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

// EnvVarAsStringSlice splits a comma-separated env var into trimmed values.
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
