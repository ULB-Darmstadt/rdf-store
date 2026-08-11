package base

import (
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

var dotEnvOnce sync.Once

// loadDotEnv loads the .env file from the working directory or one of its
// parents into the process environment. Variables already set take precedence,
// so explicitly provided environment values (e.g. via docker-compose) win.
func loadDotEnv() {
	dotEnvOnce.Do(func() {
		path := findDotEnv()
		if path == "" {
			return
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			key, value, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			key = strings.TrimSpace(key)
			if _, present := os.LookupEnv(key); present {
				continue
			}
			os.Setenv(key, unquote(strings.TrimSpace(value)))
		}
	})
}

// findDotEnv locates the nearest .env file starting at the working directory
// and walking up the directory tree.
func findDotEnv() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		path := filepath.Join(dir, ".env")
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// unquote strips matching surrounding single or double quotes from a value.
func unquote(value string) string {
	if len(value) >= 2 {
		if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
			return value[1 : len(value)-1]
		}
	}
	return value
}

var BackendUrl = EnvVar("BACKEND_URL", "http://localhost:3000")

var AuthUserHeader = "X-User"
var AuthEmailHeader = "X-Email"
var AuthGroupsHeader = "X-Groups"
var AuthWriteAccessGroup = EnvVar("WRITE_ACCESS_GROUP", "")

// EnvVar reads an environment variable and falls back to a default when unset.
// It returns the resolved string value.
func EnvVar(key string, defaultValue string) string {
	loadDotEnv()
	if val, present := os.LookupEnv(key); present {
		return val
	}
	return defaultValue
}

// EnvVarAsInt parses an environment variable into an integer with a fallback for invalid values.
// It returns the parsed integer or the default value when parsing fails.
func EnvVarAsInt(key string, defaultValue int) int {
	loadDotEnv()
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
	loadDotEnv()
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
// It returns the non-empty entries in order, or a copy of defaultValues when unset.
func EnvVarAsStringSlice(key string, defaultValues ...string) []string {
	loadDotEnv()
	var result []string
	if val, present := os.LookupEnv(key); present {
		for _, v := range strings.Split(val, ",") {
			value := strings.TrimSpace(v)
			if value != "" {
				result = append(result, value)
			}
		}
	} else {
		result = append(result, defaultValues...)
	}
	return result
}
