package shacl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"rdf-store-backend/base"
	"strings"
)

type validationResponse map[string]string

// Validate posts data and shapes to the SHACL validator service.
func Validate(shapesGraph string, shapeID string, dataGraph string, dataID string) (map[string]string, error) {
	form := url.Values{}
	form.Add("shapesGraph", shapesGraph)
	form.Add("shapeID", shapeID)
	form.Add("dataGraph", dataGraph)
	form.Add("dataID", dataID)
	client := http.Client{}
	req, err := http.NewRequest("POST", base.ValidatorEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		message := ""
		if body, err := io.ReadAll(resp.Body); err == nil {
			message = string(body)
		}
		return nil, fmt.Errorf("failed validating graph %s - status: %v, response: '%v'", dataID, resp.StatusCode, message)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var res validationResponse
	if err := json.NewDecoder(bytes.NewReader(data)).Decode(&res); err != nil {
		return nil, err
	}
	return res, nil
}
