package evalgate

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func fixture(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := NewClient(Config{APIKey: "fixture-secret", BaseURL: server.URL, OrganizationID: "42"})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestTypedReadAndHeaders(t *testing.T) {
	client := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/evaluations" || r.URL.Query().Get("limit") != "2" || r.URL.Query().Get("search") != "space & value" {
			t.Errorf("wrong URL: %s", r.URL)
		}
		if r.Header.Get("Authorization") != "Bearer fixture-secret" || r.Header.Get("X-Organization-Id") != "42" || r.Header.Get("X-EvalGate-SDK-Version") != Version {
			t.Error("missing credential/version headers")
		}
		w.Header().Set("RateLimit", `"fixture";r=9;t=60`)
		_, _ = w.Write([]byte(`{"evaluations":[],"pagination":{"hasMore":false}}`))
	})
	response, err := client.GetEvaluations(context.Background(), GetEvaluationsParams{Query: GetEvaluationsQuery{Limit: Ptr[int64](2), Search: Ptr("space & value")}})
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 || response.Headers.Get("RateLimit") == "" || !json.Valid(response.Data) {
		t.Fatal("response data/headers lost")
	}
}

func TestTypedMutationAndIdempotency(t *testing.T) {
	client := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if r.Method != "POST" || body["name"] != "release-check" || r.Header.Get("Idempotency-Key") != "once-1" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("invalid mutation: %s %#v", r.Method, body)
		}
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"id":1,"name":"release-check"}`))
	})
	_, err := client.PostEvaluations(context.Background(), PostEvaluationsParams{Body: &PostApiEvaluationsRequest{Name: "release-check"}, Options: RequestOptions{IdempotencyKey: "once-1"}})
	if err != nil {
		t.Fatal(err)
	}
}

func TestHTTPErrorAndNoRetry(t *testing.T) {
	calls := 0
	client := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Retry-After", "3")
		w.Header().Set("RateLimit", `"fixture";r=0;t=3`)
		w.Header().Set("X-Request-Id", "request-1")
		w.WriteHeader(429)
		_, _ = w.Write([]byte(`{"error":{"code":"RATE_LIMITED","message":"Slow down"}}`))
	})
	_, err := client.GetEvaluations(context.Background(), GetEvaluationsParams{})
	var apiError *APIError
	if !errors.As(err, &apiError) || apiError.StatusCode != 429 || apiError.Code != "RATE_LIMITED" || apiError.Message != "Slow down" || apiError.RequestID != "request-1" || apiError.Headers.Get("Retry-After") != "3" || calls != 1 {
		t.Fatalf("unexpected error/calls: %#v %d", err, calls)
	}
}

func TestRequiredArgumentsFailBeforeNetwork(t *testing.T) {
	calls := 0
	client := fixture(t, func(w http.ResponseWriter, r *http.Request) { calls++ })
	if _, err := client.PostEvaluations(context.Background(), PostEvaluationsParams{}); err == nil {
		t.Fatal("accepted missing body")
	}
	var data any
	if _, err := client.request(context.Background(), "get_evaluations_id", map[string]any{}, RequestOptions{}, &data); err == nil {
		t.Fatal("accepted missing path")
	}
	if _, err := client.request(context.Background(), "get_evaluations", map[string]any{"query": map[string]any{"unknown": 1}}, RequestOptions{}, &data); err == nil {
		t.Fatal("accepted unknown query")
	}
	if calls != 0 {
		t.Fatalf("made %d requests", calls)
	}
}

func TestPathEncodingDoesNotChangeQueryOrHost(t *testing.T) {
	var operationID, parameter string
	for id, operation := range operations {
		for _, name := range operation.RequiredPathParameters {
			if operation.Method == "GET" && len(operation.RequiredPathParameters) == 1 {
				operationID = id
				parameter = name
				break
			}
		}
		if operationID != "" {
			break
		}
	}
	if operationID == "" {
		t.Fatal("no path fixture")
	}
	client := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.RequestURI, "%2F") || !strings.Contains(r.RequestURI, "%3F") || !strings.Contains(r.RequestURI, "%23") || r.URL.RawQuery != "" {
			t.Errorf("unescaped path: %s", r.RequestURI)
		}
		_, _ = w.Write([]byte(`{}`))
	})
	var result any
	_, err := client.request(context.Background(), operationID, map[string]any{"path": map[string]any{parameter: "a/b?c#d"}}, RequestOptions{}, &result)
	if err != nil {
		t.Fatal(err)
	}
}

func TestRedirectDoesNotLeakBearer(t *testing.T) {
	leaked := false
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked = true }))
	defer other.Close()
	client := fixture(t, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, other.URL, 302) })
	_, err := client.GetEvaluations(context.Background(), GetEvaluationsParams{})
	var apiError *APIError
	if !errors.As(err, &apiError) || apiError.StatusCode != 302 || leaked {
		t.Fatalf("redirect followed or wrong error: %v, leak=%v", err, leaked)
	}
}

func TestPaginationAndBounds(t *testing.T) {
	var offsets []string
	client := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		offsets = append(offsets, r.URL.Query().Get("offset"))
		if r.URL.Query().Get("offset") == "1" {
			_, _ = w.Write([]byte(`{"data":[{"id":2}],"pagination":{"hasMore":false}}`))
		} else {
			_, _ = w.Write([]byte(`{"data":[{"id":1}],"pagination":{"hasMore":true}}`))
		}
	})
	pages := 0
	err := client.Paginate(context.Background(), "get_evaluations", GetEvaluationsParams{Query: GetEvaluationsQuery{Limit: Ptr[int64](1)}}, 3, func(data json.RawMessage) error { pages++; return nil })
	if err != nil || pages != 2 || !reflect.DeepEqual(offsets, []string{"", "1"}) {
		t.Fatalf("pagination failed: %v %#v %d", err, offsets, pages)
	}
	if err := client.Paginate(context.Background(), "get_evaluations", GetEvaluationsParams{}, 0, func(json.RawMessage) error { return nil }); err == nil {
		t.Fatal("accepted unbounded pagination")
	}
	if err := client.Paginate(context.Background(), "get_evaluations", GetEvaluationsParams{}, 1, func(json.RawMessage) error { return nil }); err == nil {
		t.Fatal("silently truncated pagination")
	}
}

func TestCancelledAndInvalidResponses(t *testing.T) {
	client := fixture(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("not-json")) })
	if _, err := client.GetEvaluations(context.Background(), GetEvaluationsParams{}); err == nil {
		t.Fatal("accepted invalid JSON")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.GetEvaluations(ctx, GetEvaluationsParams{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("lost cancellation: %v", err)
	}
}

func TestConfigAndContractIsolation(t *testing.T) {
	for _, config := range []Config{{}, {APIKey: "key", BaseURL: "file:///etc/passwd"}, {APIKey: "key", BaseURL: "https://user:password@example.com"}, {APIKey: "key", BaseURL: "https://example.com?other=1"}} {
		if _, err := NewClient(config); err == nil {
			t.Fatalf("accepted invalid config %#v", config)
		}
	}
	copy := Operations()
	copy["get_evaluations"].Scopes[0] = "modified"
	if operations["get_evaluations"].Scopes[0] != "eval:read" {
		t.Fatal("mutable shared contract")
	}
}

func TestArrayPaginationAndNoProgress(t *testing.T) {
	calls := 0
	client := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			_, _ = w.Write([]byte(`[{"id":1}]`))
		} else {
			_, _ = w.Write([]byte(`[]`))
		}
	})
	err := client.Paginate(context.Background(), "get_evaluations", GetEvaluationsParams{Query: GetEvaluationsQuery{Limit: Ptr[int64](1)}}, 3, func(json.RawMessage) error { return nil })
	if err != nil || calls != 2 {
		t.Fatalf("array pages failed: %v calls=%d", err, calls)
	}
	blocked := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[],"pagination":{"hasMore":true}}`))
	})
	if err := blocked.Paginate(context.Background(), "get_evaluations", GetEvaluationsParams{}, 3, func(json.RawMessage) error { return nil }); err == nil || !strings.Contains(err.Error(), "no progress") {
		t.Fatalf("no progress was accepted: %v", err)
	}
}

func TestGeneratedNullableModelRetainsNull(t *testing.T) {
	var model GetApiEvaluationsResponse200ApplicationJsonModel
	if err := json.Unmarshal([]byte(`{"description":null}`), &model); err != nil {
		t.Fatal(err)
	}
	if model.Description != nil {
		t.Fatal("nullable description did not retain null")
	}
	encoded, err := json.Marshal(model)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"description":null`) {
		t.Fatalf("null was lost: %s", encoded)
	}
}

func TestNumericParametersAndJSONIntegersRemainExact(t *testing.T) {
	if strconv.IntSize == 64 && int64(number(json.Number("9007199254740993"), 0)) != int64(9007199254740993) {
		t.Fatal("integer pagination state rounded")
	}
	client := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "GET":
			if r.URL.Path != "/api/evaluations/1000000" {
				t.Errorf("integer path changed: %s", r.URL.Path)
			}
		case "POST":
			encoded, err := io.ReadAll(r.Body)
			if err != nil || !strings.Contains(string(encoded), `"large":9007199254740993`) {
				t.Errorf("JSON integer rounded: %s (%v)", encoded, err)
			}
		}
		_, _ = w.Write([]byte(`{}`))
	})
	if _, err := client.GetEvaluationsId(context.Background(), GetEvaluationsIdParams{Path: GetEvaluationsIdPath{Id: 1000000}}); err != nil {
		t.Fatal(err)
	}
	metadata := map[string]JsonValue{"large": json.RawMessage(`9007199254740993`)}
	if _, err := client.PostTraces(context.Background(), PostTracesParams{Body: &PostApiTracesRequest{Name: "precision", Metadata: Ptr(Ptr(metadata))}}); err != nil {
		t.Fatal(err)
	}
	queryClient := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("offset") != "1000000" {
			t.Errorf("integer query changed: %s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{}`))
	})
	if _, err := queryClient.GetEvaluations(context.Background(), GetEvaluationsParams{Query: GetEvaluationsQuery{Offset: Ptr[int64](1000000)}}); err != nil {
		t.Fatal(err)
	}
}

func TestOptionalMutationPresenceSurvivesTransport(t *testing.T) {
	for _, test := range []struct {
		name, operationID, expected string
		body                        any
		path                        map[string]any
	}{
		{"omitted array", "patch_developer_api_keys_id", `{}`, PatchApiDeveloperApiKeysIdRequest{}, map[string]any{"id": 1}},
		{"empty array", "patch_developer_api_keys_id", `{"scopes":[]}`, PatchApiDeveloperApiKeysIdRequest{Scopes: Ptr([]string{})}, map[string]any{"id": 1}},
		{"empty map", "put_workflows_id_runs_runId", `{"metadata":{}}`, PutApiWorkflowsIdRunsRunIdRequest{Metadata: Ptr(map[string]JsonValue{})}, map[string]any{"id": 1, "runId": 2}},
		{"omitted nullable", "put_workflows_id", `{}`, PutApiWorkflowsIdRequest{}, map[string]any{"id": 1}},
		{"explicit null", "put_workflows_id", `{"description":null}`, PutApiWorkflowsIdRequest{Description: Ptr((*string)(nil))}, map[string]any{"id": 1}},
		{"nullable value", "put_workflows_id", `{"description":"updated"}`, PutApiWorkflowsIdRequest{Description: Ptr(Ptr("updated"))}, map[string]any{"id": 1}},
		{"null collection", "post_traces", `{"metadata":null,"name":"nullable"}`, PostApiTracesRequest{Name: "nullable", Metadata: Ptr((*map[string]JsonValue)(nil))}, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := fixture(t, func(w http.ResponseWriter, r *http.Request) {
				encoded, err := io.ReadAll(r.Body)
				var actual, expected any
				if err != nil || json.Unmarshal(encoded, &actual) != nil || json.Unmarshal([]byte(test.expected), &expected) != nil || !reflect.DeepEqual(actual, expected) {
					t.Errorf("presence changed: got %s, expected %s (%v)", encoded, test.expected, err)
				}
				_, _ = w.Write([]byte(`{}`))
			})
			var result any
			if _, err := client.request(context.Background(), test.operationID, map[string]any{"path": test.path, "body": test.body}, RequestOptions{}, &result); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestOptionalNullableModelDecodeEncodeRoundTrip(t *testing.T) {
	for _, test := range []struct {
		name, encoded string
		model         any
	}{
		{"omitted scalar", `{}`, new(PutApiWorkflowsIdRequest)},
		{"null scalar", `{"description":null}`, new(PutApiWorkflowsIdRequest)},
		{"scalar value", `{"description":"updated"}`, new(PutApiWorkflowsIdRequest)},
		{"omitted map", `{"name":"trace"}`, new(PostApiTracesRequest)},
		{"null map", `{"name":"trace","metadata":null}`, new(PostApiTracesRequest)},
		{"empty map", `{"name":"trace","metadata":{}}`, new(PostApiTracesRequest)},
		{"map value", `{"name":"trace","metadata":{"count":1}}`, new(PostApiTracesRequest)},
		{"nullable JSON alias", `{"name":"span","metadata":null}`, new(PostApiTracesIdSpansRequest)},
		{"omitted array", `{"input":"case"}`, new(PostApiSyncLabeledCasesRequestModelCasesItem)},
		{"null array", `{"input":"case","standardsRefs":null}`, new(PostApiSyncLabeledCasesRequestModelCasesItem)},
		{"empty array", `{"input":"case","standardsRefs":[]}`, new(PostApiSyncLabeledCasesRequestModelCasesItem)},
		{"array value", `{"input":"case","standardsRefs":["standard-1"]}`, new(PostApiSyncLabeledCasesRequestModelCasesItem)},
		{"nested nullable collections", `{"evaluationId":1,"cases":[{"input":"case","standardsRefs":null,"metadata":null}]}`, new(PostApiSyncLabeledCasesRequest)},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := json.Unmarshal([]byte(test.encoded), test.model); err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(test.model)
			var actual, expected any
			if err != nil || json.Unmarshal(encoded, &actual) != nil || json.Unmarshal([]byte(test.encoded), &expected) != nil || !reflect.DeepEqual(actual, expected) {
				t.Fatalf("model round trip changed presence: got %s, expected %s (%v)", encoded, test.encoded, err)
			}
		})
	}
}
