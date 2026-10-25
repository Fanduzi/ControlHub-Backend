// Package api provides HTTP handlers and JSON request parsing for the ControlHub REST API.
package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

func decodeJSONBody(r *http.Request, target any) error {
	return decodeJSON(r.Body, target)
}

func decodeJSON(body io.Reader, target any) error {
	decoder := json.NewDecoder(body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("multiple JSON values are not allowed")
		}
		return err
	}
	return nil
}
