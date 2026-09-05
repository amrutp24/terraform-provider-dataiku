package dataiku

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
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

// asAPIError is a tiny local helper so the test file does not need to import
// the errors package just for one assertion.
func asAPIError(err error, target **APIError) bool {
	if e, ok := err.(*APIError); ok {
		*target = e
		return true
	}
	return false
}
