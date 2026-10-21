// Package evalgate is the official generated EvalGate REST client.
package evalgate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client does not retry mutations. Use RequestOptions.IdempotencyKey when retrying.
type Client struct {
	baseURL                *url.URL
	apiKey, organizationID string
	httpClient             *http.Client
}

func (c *Client) String() string { return "EvalGate Client(" + c.baseURL.String() + ")" }

type Config struct {
	APIKey, BaseURL, OrganizationID string
	HTTPClient                      *http.Client
}
type RequestOptions struct{ IdempotencyKey string }
type Response[T any] struct {
	Data       T
	StatusCode int
	Headers    http.Header
}

// APIError retains structured errors and live rate-limit/Retry-After headers.
type APIError struct {
	StatusCode               int
	Code, Message, RequestID string
	Headers                  http.Header
	Data                     json.RawMessage
}

func (e *APIError) Error() string {
	return fmt.Sprintf("EvalGate HTTP %d (%s): %s", e.StatusCode, e.Code, e.Message)
}
func Ptr[T any](value T) *T { return &value }

type Operation struct {
	ID                       string   `json:"operationId"`
	Method                   string   `json:"method"`
	Path                     string   `json:"path"`
	PathParameters           []string `json:"pathParameters"`
	QueryParameters          []string `json:"queryParameters"`
	HeaderParameters         []string `json:"headerParameters"`
	RequiredPathParameters   []string `json:"requiredPathParameters"`
	RequiredQueryParameters  []string `json:"requiredQueryParameters"`
	RequiredHeaderParameters []string `json:"requiredHeaderParameters"`
	RequiredBodyProperties   []string `json:"requiredBodyProperties"`
	BodyRequired             bool     `json:"requestBodyRequired"`
	BodyAllowed              bool     `json:"requestBodyAllowed"`
	Pagination               string   `json:"pagination"`
	Scopes                   []string `json:"scopes"`
}

var operations = func() map[string]Operation {
	var rows []Operation
	if err := json.Unmarshal(operationJSON, &rows); err != nil {
		panic(err)
	}
	result := make(map[string]Operation, len(rows))
	for _, row := range rows {
		result[row.ID] = row
	}
	return result
}()

// Operations returns a copy of the generated method/path/scope contracts.
func Operations() map[string]Operation {
	data, _ := json.Marshal(operations)
	var copy map[string]Operation
	_ = json.Unmarshal(data, &copy)
	return copy
}

func NewClient(config Config) (*Client, error) {
	if strings.TrimSpace(config.APIKey) == "" {
		return nil, errors.New("EvalGate API key is required")
	}
	if config.BaseURL == "" {
		config.BaseURL = "https://www.evalgate.com"
	}
	base, err := url.Parse(config.BaseURL)
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return nil, errors.New("EvalGate base URL must be an absolute HTTP(S) origin or path without credentials, query, or fragment")
	}
	client := config.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	// Refuse redirects: bearer credentials and mutation bodies must stay on the configured origin.
	copy := *client
	copy.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{baseURL: base, apiKey: strings.TrimSpace(config.APIKey), organizationID: config.OrganizationID, httpClient: &copy}, nil
}

func object(value any) (map[string]any, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	err = decoder.Decode(&result)
	return result, err
}

// decodeModelJSON uses the typed decoder first, then records explicit nulls for
// generated optional fields. encoding/json otherwise clears the outer pointer
// of **T, making a present null indistinguishable from an omitted property.
func decodeModelJSON(data []byte, target any, nullableFields ...string) (map[string]bool, error) {
	if err := json.Unmarshal(data, target); err != nil {
		return nil, err
	}
	var properties map[string]json.RawMessage
	if err := json.Unmarshal(data, &properties); err != nil {
		return nil, err
	}
	nulls := make(map[string]bool)
	for _, name := range nullableFields {
		if raw, present := properties[name]; present && bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			nulls[name] = true
		}
	}
	return nulls, nil
}

func requireKeys(values map[string]any, keys []string, location string) error {
	for _, key := range keys {
		if value, ok := values[key]; !ok || value == nil || value == "" {
			return fmt.Errorf("missing required %s parameter %s", location, key)
		}
	}
	return nil
}
func allowedKeys(values map[string]any, keys []string, location string) error {
	for key := range values {
		found := false
		for _, allowed := range keys {
			if key == allowed {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("unknown %s parameter %s", location, key)
		}
	}
	return nil
}
func parameterValues(value any) []string {
	if array, ok := value.([]any); ok {
		var values []string
		for _, item := range array {
			values = append(values, parameterValues(item)...)
		}
		return values
	}
	if _, ok := value.(map[string]any); ok {
		encoded, _ := json.Marshal(value)
		return []string{string(encoded)}
	}
	return []string{fmt.Sprint(value)}
}

func (c *Client) request(ctx context.Context, operationID string, params any, options RequestOptions, destination any) (*http.Response, error) {
	operation, ok := operations[operationID]
	if !ok {
		return nil, fmt.Errorf("unknown EvalGate operation %s", operationID)
	}
	values, err := object(params)
	if err != nil {
		return nil, err
	}
	groups := map[string]map[string]any{}
	for _, location := range []string{"path", "query", "header"} {
		group, _ := values[location].(map[string]any)
		if group == nil {
			group = map[string]any{}
		}
		groups[location] = group
	}
	for _, group := range []struct {
		location          string
		allowed, required []string
	}{{"path", operation.PathParameters, operation.RequiredPathParameters}, {"query", operation.QueryParameters, operation.RequiredQueryParameters}, {"header", operation.HeaderParameters, operation.RequiredHeaderParameters}} {
		if err := allowedKeys(groups[group.location], group.allowed, group.location); err != nil {
			return nil, err
		}
		if err := requireKeys(groups[group.location], group.required, group.location); err != nil {
			return nil, err
		}
	}
	body, hasBody := values["body"]
	if operation.BodyRequired && (!hasBody || body == nil) {
		return nil, errors.New("required request body is missing")
	}
	if !operation.BodyAllowed && hasBody {
		return nil, errors.New("operation does not accept a request body")
	}
	if hasBody && len(operation.RequiredBodyProperties) > 0 {
		bodyObject, ok := body.(map[string]any)
		if !ok {
			return nil, errors.New("request body must be an object")
		}
		for _, key := range operation.RequiredBodyProperties {
			if _, exists := bodyObject[key]; !exists {
				return nil, fmt.Errorf("missing required body property %s", key)
			}
		}
	}
	endpoint := operation.Path
	for key, value := range groups["path"] {
		segment := fmt.Sprint(value)
		if segment == "." || segment == ".." {
			return nil, errors.New("dot path segments are not allowed")
		}
		endpoint = strings.ReplaceAll(endpoint, "{"+key+"}", url.PathEscape(segment))
	}
	if strings.Contains(endpoint, "{") {
		return nil, errors.New("unresolved path template")
	}
	// Parse the escaped path without letting an untrusted parameter change host or query.
	requestURL, err := url.Parse(strings.TrimRight(c.baseURL.String(), "/") + endpoint)
	if err != nil {
		return nil, err
	}
	query := requestURL.Query()
	for key, value := range groups["query"] {
		if value != nil {
			for _, item := range parameterValues(value) {
				query.Add(key, item)
			}
		}
	}
	requestURL.RawQuery = query.Encode()
	var payload io.Reader
	if hasBody {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		payload = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, operation.Method, requestURL.String(), payload)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+c.apiKey)
	request.Header.Set("X-EvalGate-SDK-Version", Version)
	request.Header.Set("X-EvalGate-Spec-Version", SpecVersion)
	if c.organizationID != "" {
		request.Header.Set("X-Organization-Id", c.organizationID)
	}
	for key, value := range groups["header"] {
		if strings.EqualFold(key, "authorization") || strings.EqualFold(key, "host") || strings.EqualFold(key, "cookie") {
			return nil, fmt.Errorf("credential header %s cannot be overridden", key)
		}
		request.Header.Set(key, fmt.Sprint(value))
	}
	if hasBody {
		request.Header.Set("Content-Type", "application/json")
	}
	if options.IdempotencyKey != "" {
		request.Header.Set("Idempotency-Key", options.IdempotencyKey)
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	const maxBytes = 32 << 20
	encoded, err := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(encoded) > maxBytes {
		return nil, errors.New("EvalGate response exceeds 32 MiB limit")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var data struct {
			Code    string          `json:"code"`
			Message string          `json:"message"`
			Error   json.RawMessage `json:"error"`
		}
		_ = json.Unmarshal(encoded, &data)
		if len(data.Error) > 0 {
			var message string
			if json.Unmarshal(data.Error, &message) == nil {
				data.Message = message
			} else {
				var nested struct{ Code, Message string }
				if json.Unmarshal(data.Error, &nested) == nil {
					data.Message = nested.Message
					if data.Code == "" {
						data.Code = nested.Code
					}
				}
			}
		}
		if data.Message == "" {
			data.Message = http.StatusText(response.StatusCode)
		}
		return nil, &APIError{StatusCode: response.StatusCode, Code: data.Code, Message: data.Message, RequestID: response.Header.Get("X-Request-Id"), Headers: response.Header.Clone(), Data: encoded}
	}
	if len(bytes.TrimSpace(encoded)) > 0 {
		if err := json.Unmarshal(encoded, destination); err != nil {
			return nil, fmt.Errorf("invalid EvalGate JSON response: %w", err)
		}
	}
	return response, nil
}

// Paginate visits raw JSON pages for any generated paginated operation. Typed
// one-page methods remain available. Bounds and repeated cursors fail explicitly.
func (c *Client) Paginate(ctx context.Context, operationID string, params any, maxPages int, visit func(json.RawMessage) error) error {
	operation, ok := operations[operationID]
	if !ok || operation.Pagination == "" {
		return errors.New("operation does not declare pagination")
	}
	if maxPages < 1 || visit == nil {
		return errors.New("positive maxPages and visit callback are required")
	}
	values, err := object(params)
	if err != nil {
		return err
	}
	query, _ := values["query"].(map[string]any)
	if query == nil {
		query = map[string]any{}
		values["query"] = query
	}
	seen := map[string]bool{}
	for page := 0; page < maxPages; page++ {
		var raw json.RawMessage
		if _, err := c.request(ctx, operationID, values, RequestOptions{}, &raw); err != nil {
			return err
		}
		if err := visit(raw); err != nil {
			return err
		}
		var decoded any
		if json.Unmarshal(raw, &decoded) != nil {
			return errors.New("pagination requires JSON")
		}
		data, isObject := decoded.(map[string]any)
		array, isArray := decoded.([]any)
		if !isObject && !isArray {
			return errors.New("pagination requires an object or array response")
		}
		if data == nil {
			data = map[string]any{}
		}
		state := data
		if nested, ok := data["pagination"].(map[string]any); ok {
			state = nested
		} else if meta, ok := data["meta"].(map[string]any); ok {
			state = meta
			if nested, ok := meta["pagination"].(map[string]any); ok {
				state = nested
			}
		}
		more, hasMore := state["hasMore"]
		if !hasMore {
			more, hasMore = state["has_more"]
		}
		if more == false {
			return nil
		}
		if operation.Pagination == "cursor" {
			cursor, _ := state["nextCursor"].(string)
			if cursor == "" {
				cursor, _ = state["next_cursor"].(string)
			}
			if cursor == "" {
				return nil
			}
			if seen[cursor] {
				return errors.New("pagination repeated cursor")
			}
			seen[cursor] = true
			query["cursor"] = cursor
			continue
		}
		limit := number(query["limit"], number(state["limit"], 50))
		if limit < 1 {
			return errors.New("pagination limit must be positive")
		}
		count := -1
		if isArray {
			count = len(array)
		}
		for _, key := range []string{"data", "items", "results", "evaluations", "traces"} {
			if items, ok := data[key].([]any); ok {
				count = len(items)
				break
			}
		}
		if !hasMore && count >= 0 && count < limit {
			return nil
		}
		if operation.Pagination == "offset" {
			offset := number(query["offset"], number(state["offset"], 0))
			step := limit
			if count >= 0 {
				step = count
			}
			if step == 0 {
				return errors.New("pagination made no progress")
			}
			next := offset + step
			if total, ok := state["total"]; ok && next >= number(total, 0) {
				return nil
			}
			query["offset"] = next
		} else {
			current := number(query["page"], number(state["page"], 1))
			total := number(state["totalPages"], number(state["total_pages"], 0))
			if total > 0 && current >= total {
				return nil
			}
			query["page"] = current + 1
		}
	}
	return errors.New("pagination exceeded maxPages")
}
func number(value any, fallback int) int {
	if value == nil {
		return fallback
	}
	// Decoder.UseNumber retains decimal integers; avoid rounding them while
	// advancing pagination state as well as when sending a single request.
	if integer, err := strconv.Atoi(fmt.Sprint(value)); err == nil {
		return integer
	}
	number, err := strconv.ParseFloat(fmt.Sprint(value), 64)
	if err != nil {
		return fallback
	}
	return int(number)
}
