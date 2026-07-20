package rdf

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// isValidIRI validates that a value is a URL-like IRI.
// It returns true when parsing succeeds and a scheme is present.
func isValidIRI(value string) bool {
	u, err := url.Parse(value)
	return err == nil && u.Scheme != ""
}

// doRequest executes an HTTP request and reads the response body.
// It returns the status code, response bytes, and any error encountered.
func doRequest(req *http.Request) (int, []byte, error) {
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, data, nil
}

// newHTTPError formats an HTTP error message using context, status, and body.
// It returns an error with a formatted message.
func newHTTPError(context string, status int, body []byte) error {
	message := strings.TrimSpace(string(body))
	if message == "" {
		return fmt.Errorf("%s - status: %d", context, status)
	}
	return fmt.Errorf("%s - status: %d, response: %q", context, status, message)
}

// statusIsOK checks whether a status code is a success response.
// It returns true when the status is in the 2xx range.
func statusIsOK(status int) bool {
	return status >= 200 && status <= 299
}

func arrayToSparqlValues(array []string) string {
	var builder strings.Builder
	for _, s := range array {
		builder.WriteString("<")
		builder.WriteString(s)
		builder.WriteString("> ")
	}
	return builder.String()
}
