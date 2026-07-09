package updater

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const (
	maxJSONBodyBytes        int64 = 1 << 20
	maxSmallJSONBodyBytes   int64 = 64 << 10
	maxActionJSONBodyBytes  int64 = 64 << 10
	maxPackageListBodyBytes int64 = 256 << 10
)

type apiErrorResponse struct {
	Error string `json:"error"`
}

type logsAPIResponse struct {
	Entries      []LogEntry `json:"entries"`
	OldestID     int64      `json:"oldest_id"`
	LatestID     int64      `json:"latest_id"`
	DroppedCount int64      `json:"dropped_count"`
	DroppedBytes int64      `json:"dropped_bytes"`
	GapDetected  bool       `json:"gap_detected"`
}

type commandAPIResponse struct {
	Result         *CommandResult  `json:"result,omitempty"`
	RefreshStarted bool            `json:"refresh_started,omitempty"`
	Settings       *StatusSettings `json:"settings,omitempty"`
	Notice         string          `json:"notice,omitempty"`
}

type oneOrManyStrings []string

func (list *oneOrManyStrings) UnmarshalJSON(data []byte) error {
	var many []string
	if err := json.Unmarshal(data, &many); err == nil {
		*list = many
		return nil
	}
	var one string
	if err := json.Unmarshal(data, &one); err == nil {
		*list = []string{one}
		return nil
	}
	if strings.TrimSpace(string(data)) == "null" {
		*list = nil
		return nil
	}
	return fmt.Errorf("expected string or string array")
}

func combineStringLists(lists ...oneOrManyStrings) []string {
	var combined []string
	for _, list := range lists {
		combined = append(combined, list...)
	}
	return combined
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeAPIError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, apiErrorResponse{Error: message})
}

func requireMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method == method {
		return true
	}
	writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
	return false
}

func commandResponse(result CommandResult) commandAPIResponse {
	return commandAPIResponse{Result: &result}
}

func refreshedCommandResponse(result CommandResult) commandAPIResponse {
	return commandAPIResponse{Result: &result, RefreshStarted: true}
}

func settingsResponse(state State) commandAPIResponse {
	settings := statusSettingsFromState(state)
	return commandAPIResponse{Settings: &settings}
}

func settingsCommandResponse(state State, result CommandResult) commandAPIResponse {
	settings := statusSettingsFromState(state)
	return commandAPIResponse{Result: &result, Settings: &settings}
}

func requestIsJSON(r *http.Request) bool {
	return strings.Contains(strings.ToLower(r.Header.Get("Content-Type")), "application/json")
}

func decodeJSONRequest(r *http.Request, target any) error {
	return decodeJSONRequestBounded(r, target, maxJSONBodyBytes)
}

func decodeSmallJSONRequest(r *http.Request, target any) error {
	return decodeJSONRequestBounded(r, target, maxSmallJSONBodyBytes)
}

func decodeActionJSONRequest(r *http.Request, target any) error {
	return decodeJSONRequestBounded(r, target, maxActionJSONBodyBytes)
}

func decodePackageListJSONRequest(r *http.Request, target any) error {
	return decodeJSONRequestBounded(r, target, maxPackageListBodyBytes)
}

func decodeJSONRequestBounded(r *http.Request, target any, maxBytes int64) error {
	body, err := readBoundedRequestBody(r, maxBytes)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid JSON body: %w", err)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("invalid JSON body: trailing data")
		}
		return fmt.Errorf("invalid JSON body: %w", err)
	}
	return nil
}

func decodeRawJSONMapRequestBounded(r *http.Request, target *map[string]json.RawMessage, maxBytes int64) error {
	body, err := readBoundedRequestBody(r, maxBytes)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid JSON body: %w", err)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("invalid JSON body: trailing data")
		}
		return fmt.Errorf("invalid JSON body: %w", err)
	}
	return nil
}

func readBoundedRequestBody(r *http.Request, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		return nil, fmt.Errorf("invalid request body limit")
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read request body: %w", err)
	}
	if int64(len(body)) > maxBytes {
		return nil, fmt.Errorf("request body too large")
	}
	return body, nil
}

func parseFormRequest(r *http.Request) error {
	if err := r.ParseForm(); err != nil {
		return fmt.Errorf("invalid form body: %w", err)
	}
	return nil
}
