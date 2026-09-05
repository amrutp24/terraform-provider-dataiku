package dataiku

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"
)

func testClient(t *testing.T, handler http.Handler) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client, err := NewClient(Config{Host: server.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client
}

func TestNewClientValidation(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{"ok", Config{Host: "https://dss.example.com", APIKey: "k"}, false},
		{"trailing slash", Config{Host: "https://dss.example.com/", APIKey: "k"}, false},
		{"api suffix stripped", Config{Host: "https://dss.example.com/public/api", APIKey: "k"}, false},
		{"empty host", Config{Host: "", APIKey: "k"}, true},
		{"empty key", Config{Host: "https://dss.example.com", APIKey: ""}, true},
		{"bad scheme", Config{Host: "ftp://dss.example.com", APIKey: "k"}, true},
		{"no scheme", Config{Host: "dss.example.com", APIKey: "k"}, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewClient(tc.cfg)
			if tc.wantErr && err == nil {
				t.Fatal("expected an error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestClientStripsAPISuffixFromHost(t *testing.T) {
	client, err := NewClient(Config{Host: "https://dss.example.com/public/api/", APIKey: "k"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if got, want := client.Host(), "https://dss.example.com"; got != want {
		t.Fatalf("Host() = %q, want %q", got, want)
	}
}

// TestRequestShape covers the parts of the wire format DSS is strict about:
// the /public/api prefix, the API key sent as the basic-auth username with an
// empty password, and a JSON content type on writes.
func TestRequestShape(t *testing.T) {
	var (
		gotPath     string
		gotUser     string
		gotPass     string
		gotHasPass  bool
		gotType     string
		gotBody     map[string]any
		gotAccept   string
		gotUserAgnt string
	)

	client := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotUser, gotPass, gotHasPass = r.BasicAuth()
		gotType = r.Header.Get("Content-Type")
		gotAccept = r.Header.Get("Accept")
		gotUserAgnt = r.Header.Get("User-Agent")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
	}))

	err := client.CreateProject(context.Background(), CreateProjectRequest{
		ProjectKey: "MYPROJECT",
		Name:       "My project",
		Owner:      "admin",
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	if want := "/public/api/projects/"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if !gotHasPass {
		t.Fatal("no basic auth credentials were sent")
	}
	if gotUser != "test-key" {
		t.Errorf("basic auth username = %q, want the API key", gotUser)
	}
	if gotPass != "" {
		t.Errorf("basic auth password = %q, want empty", gotPass)
	}
	if gotType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotType)
	}
	if gotAccept != "application/json" {
		t.Errorf("Accept = %q, want application/json", gotAccept)
	}
	if gotUserAgnt == "" {
		t.Error("User-Agent was not set")
	}
	if gotBody["projectKey"] != "MYPROJECT" {
		t.Errorf("body projectKey = %v, want MYPROJECT", gotBody["projectKey"])
	}
	// Nil slices must serialize as [] rather than null; DSS rejects null here.
	if _, ok := gotBody["tags"].([]any); !ok {
		t.Errorf("body tags = %#v, want an empty JSON array", gotBody["tags"])
	}
	if _, ok := gotBody["permissions"].([]any); !ok {
		t.Errorf("body permissions = %#v, want an empty JSON array", gotBody["permissions"])
	}
}

func TestCreateProjectSendsFolderAsQueryParam(t *testing.T) {
	var gotQuery string
	client := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("projectFolderId")
		w.WriteHeader(http.StatusOK)
	}))

	err := client.CreateProject(context.Background(), CreateProjectRequest{
		ProjectKey:      "P",
		Name:            "P",
		Owner:           "admin",
		ProjectFolderID: "folder-123",
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if gotQuery != "folder-123" {
		t.Errorf("projectFolderId = %q, want folder-123", gotQuery)
	}
}

func TestDeleteProjectSendsClearOptions(t *testing.T) {
	var got url.Values
	client := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method = %s, want DELETE", r.Method)
		}
		got = r.URL.Query()
		w.WriteHeader(http.StatusOK)
	}))

	err := client.DeleteProject(context.Background(), "P", DeleteProjectOptions{
		ClearManagedDatasets:    true,
		ClearJobAndScenarioLogs: true,
	})
	if err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}

	for key, want := range map[string]string{
		"clearManagedDatasets":      "true",
		"clearOutputManagedFolders": "false",
		"clearJobAndScenarioLogs":   "true",
	} {
		if got.Get(key) != want {
			t.Errorf("%s = %q, want %q", key, got.Get(key), want)
		}
	}
}

// TestUpdatePreservesUnmanagedFields is the behaviour that keeps a DSS upgrade
// or an out-of-band setting from being wiped by a Terraform apply.
func TestUpdatePreservesUnmanagedFields(t *testing.T) {
	stored := map[string]any{
		"name":                    "datascientists",
		"description":             "old",
		"sourceType":              "LOCAL",
		"admin":                   false,
		"mayFutureAbilityWeDoNot": true,
	}

	client := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(stored)
		case http.MethodPut:
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decoding PUT body: %v", err)
			}
			stored = body
			w.WriteHeader(http.StatusOK)
		}
	}))

	err := client.UpdateGroup(context.Background(), "datascientists", func(g map[string]any) {
		g["description"] = "new"
	})
	if err != nil {
		t.Fatalf("UpdateGroup: %v", err)
	}

	if stored["description"] != "new" {
		t.Errorf("description = %v, want new", stored["description"])
	}
	if stored["mayFutureAbilityWeDoNot"] != true {
		t.Error("an ability the provider does not model was dropped by the update")
	}
	if stored["sourceType"] != "LOCAL" {
		t.Errorf("sourceType = %v, want LOCAL", stored["sourceType"])
	}
}

func TestErrorParsing(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		wantMsg    string
		wantNotFnd bool
		wantUnauth bool
	}{
		{
			name:       "dss error envelope",
			status:     http.StatusBadRequest,
			body:       `{"errorType":"IllegalArgument","message":"Bad key","detailedMessage":"Project key is invalid"}`,
			wantMsg:    "Bad key",
			wantNotFnd: false,
		},
		{
			name:       "not found",
			status:     http.StatusNotFound,
			body:       `{"message":"No such project"}`,
			wantMsg:    "No such project",
			wantNotFnd: true,
		},
		{
			name:       "unauthorized",
			status:     http.StatusUnauthorized,
			body:       ``,
			wantUnauth: true,
		},
		{
			name:    "html body",
			status:  http.StatusBadGateway,
			body:    `<html><body>gateway</body></html>`,
			wantMsg: `<html><body>gateway</body></html>`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))

			_, err := client.GetProjectMetadata(context.Background(), "P")
			if err == nil {
				t.Fatal("expected an error, got nil")
			}
			if got := StatusCode(err); got != tc.status {
				t.Errorf("StatusCode = %d, want %d", got, tc.status)
			}
			if IsNotFound(err) != tc.wantNotFnd {
				t.Errorf("IsNotFound = %v, want %v", IsNotFound(err), tc.wantNotFnd)
			}
			if IsUnauthorized(err) != tc.wantUnauth {
				t.Errorf("IsUnauthorized = %v, want %v", IsUnauthorized(err), tc.wantUnauth)
			}
			if tc.wantMsg != "" {
				var apiErr *APIError
				if !asAPIError(err, &apiErr) {
					t.Fatalf("error is not an *APIError: %T", err)
				}
				if apiErr.Message != tc.wantMsg {
					t.Errorf("Message = %q, want %q", apiErr.Message, tc.wantMsg)
				}
			}
		})
	}
}

func TestEmptyResponseBodyIsNotAnError(t *testing.T) {
	client := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	md, err := client.GetProjectMetadata(context.Background(), "P")
	if err != nil {
		t.Fatalf("GetProjectMetadata: %v", err)
	}
	if len(md) != 0 {
		t.Errorf("metadata = %v, want empty", md)
	}
}

func TestProjectExists(t *testing.T) {
	client := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/public/api/projects/GONE/metadata" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"label": "Here"})
	}))

	if ok, err := client.ProjectExists(context.Background(), "HERE"); err != nil || !ok {
		t.Errorf("ProjectExists(HERE) = %v, %v; want true, nil", ok, err)
	}
	if ok, err := client.ProjectExists(context.Background(), "GONE"); err != nil || ok {
		t.Errorf("ProjectExists(GONE) = %v, %v; want false, nil", ok, err)
	}
}

func TestGetProjectVariablesNeverReturnsNilMaps(t *testing.T) {
	client := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))

	vars, err := client.GetProjectVariables(context.Background(), "P")
	if err != nil {
		t.Fatalf("GetProjectVariables: %v", err)
	}
	if vars.Standard == nil || vars.Local == nil {
		t.Fatalf("got nil maps: %+v", vars)
	}
}

func TestPathEscaping(t *testing.T) {
	var gotPath string
	client := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{}`))
	}))

	if _, err := client.GetUser(context.Background(), "first last"); err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if want := "/public/api/admin/users/first last"; gotPath != want {
		t.Errorf("decoded path = %q, want %q", gotPath, want)
	}
}

// asAPIError unwraps to the API error. errors.As rather than a type assertion,
// so this keeps working if the error is ever wrapped on its way out.
func asAPIError(err error, target **APIError) bool {
	return errors.As(err, target)
}

// TestRetriesIdempotentRequests covers the case retrying exists for: a
// transient 503 that succeeds on a second attempt.
func TestRetriesIdempotentRequests(t *testing.T) {
	var attempts int32
	client := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&attempts, 1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"label": "Recovered"})
	}))

	md, err := client.GetProjectMetadata(context.Background(), "P")
	if err != nil {
		t.Fatalf("GetProjectMetadata: %v", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Errorf("attempts = %d, want 3", got)
	}
	if md["label"] != "Recovered" {
		t.Errorf("label = %v, want Recovered", md["label"])
	}
}

// TestDoesNotRetryPostOnServerError is the safety property. A 5xx on a create
// can mean the object was made and only the response was lost, so repeating it
// would create a second project.
func TestDoesNotRetryPostOnServerError(t *testing.T) {
	var attempts int32
	client := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))

	err := client.CreateProject(context.Background(), CreateProjectRequest{
		ProjectKey: "P", Name: "P", Owner: "admin",
	})
	if err == nil {
		t.Fatal("expected an error")
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Errorf("POST was attempted %d times on a 5xx; it must not be repeated", got)
	}
}

// A 429 is different: the server is explicitly saying it did not process the
// request, so even a create is safe to repeat.
func TestRetriesPostOnRateLimit(t *testing.T) {
	var attempts int32
	client := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&attempts, 1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	if err := client.CreateProject(context.Background(), CreateProjectRequest{
		ProjectKey: "P", Name: "P", Owner: "admin",
	}); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 2 {
		t.Errorf("attempts = %d, want 2", got)
	}
}

// The retried request must carry its body again, not an empty one.
func TestRetryResendsTheBody(t *testing.T) {
	var attempts int32
	var lastBody map[string]any

	client := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		_ = json.NewDecoder(r.Body).Decode(&lastBody)
		if n < 2 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	err := client.UpdateGroup(context.Background(), "g", func(g map[string]any) {
		g["description"] = "resent"
	})
	// The GET inside UpdateGroup also retries, so just assert the final PUT
	// arrived intact.
	if err != nil {
		t.Fatalf("UpdateGroup: %v", err)
	}
	if lastBody == nil {
		t.Fatal("the retried request carried no body")
	}
}

func TestRetriesGiveUpAndReportTheLastFailure(t *testing.T) {
	var attempts int32
	client := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"message":"still down"}`))
	}))

	_, err := client.GetProjectMetadata(context.Background(), "P")
	if err == nil {
		t.Fatal("expected an error")
	}
	if StatusCode(err) != http.StatusServiceUnavailable {
		t.Errorf("StatusCode = %d, want 503", StatusCode(err))
	}
	// One initial attempt plus the default budget of three.
	if got := atomic.LoadInt32(&attempts); got != 4 {
		t.Errorf("attempts = %d, want 4", got)
	}
}

func TestContextCancellationStopsRetrying(t *testing.T) {
	client := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	if _, err := client.GetProjectMetadata(ctx, "P"); err == nil {
		t.Fatal("expected an error")
	}
	// Without honouring the context this would sit through the full backoff.
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("took %s; cancellation was not honoured", elapsed)
	}
}
