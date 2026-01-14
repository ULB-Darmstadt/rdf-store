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

type validationResponse struct {
	Conforms bool `json:"conforms"`
}

func Validate(shapesGraph string, shapeID string, dataGraph string, dataID string) (bool, error) {
	form := url.Values{}
	form.Add("shapesGraph", shapesGraph)
	form.Add("shapeID", shapeID)
	form.Add("dataGraph", dataGraph)
	form.Add("dataID", dataID)
	client := http.Client{}
	req, err := http.NewRequest("POST", base.ValidatorEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return false, err
	}
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		message := ""
		if body, err := io.ReadAll(resp.Body); err == nil {
			message = string(body)
		}
		return false, fmt.Errorf("failed validating graph %s - status: %v, response: '%v'", dataID, resp.StatusCode, message)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, err
	}
	var res validationResponse
	if err := json.NewDecoder(bytes.NewReader(data)).Decode(&res); err != nil {
		return false, err
	}
	return res.Conforms, nil
}
