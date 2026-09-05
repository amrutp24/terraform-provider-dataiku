package provider

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeDSS is an in-memory stand-in for a DSS instance. It implements just
// enough of the public API for the acceptance tests to drive a real terraform
// binary through create, read, update, import and destroy without needing an
// instance to point at.
type fakeDSS struct {
	mu sync.Mutex

	projects    map[string]map[string]any
	permissions map[string]map[string]any
	variables   map[string]map[string]any
	groups      map[string]map[string]any
	users       map[string]map[string]any
	connections map[string]map[string]any
	codeEnvs    map[string]map[string]any
	scenarios   map[string]map[string]any
	payloads    map[string]string

	// unmodelledFieldDropped records whether an update ever wrote back a
	// document that lost a field the provider does not model.
	unmodelledFieldDropped bool
}

func newFakeDSS(t *testing.T) (*fakeDSS, string) {
	t.Helper()

	f := &fakeDSS{
		projects:    map[string]map[string]any{},
		permissions: map[string]map[string]any{},
		variables:   map[string]map[string]any{},
		groups:      map[string]map[string]any{},
		users:       map[string]map[string]any{},
		connections: map[string]map[string]any{},
		codeEnvs:    map[string]map[string]any{},
		scenarios:   map[string]map[string]any{},
		payloads:    map[string]string{},
	}

	server := httptest.NewServer(f.handler())
	t.Cleanup(server.Close)
	return f, server.URL
}

func (f *fakeDSS) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/public/api/auth/info", f.handleAuthInfo)
	mux.HandleFunc("/public/api/projects/", f.handleProjects)
	mux.HandleFunc("/public/api/admin/groups/", f.handleGroups)
	mux.HandleFunc("/public/api/admin/users/", f.handleUsers)
	mux.HandleFunc("/public/api/admin/connections/", f.handleConnections)
	mux.HandleFunc("/public/api/admin/code-envs/", f.handleCodeEnvs)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeErr(w, http.StatusNotFound, "unhandled "+r.Method+" "+r.URL.Path)
	})
	return mux
}

func (f *fakeDSS) handleAuthInfo(w http.ResponseWriter, r *http.Request) {
	// DSS expects the API key as the basic-auth username with an empty
	// password, so reject anything else to keep the provider honest.
	user, pass, ok := r.BasicAuth()
	if !ok || user == "" || pass != "" {
		writeErr(w, http.StatusUnauthorized, "bad credentials")
		return
	}
	writeJSON(w, map[string]any{"authIdentifier": "admin", "admin": true})
}

func (f *fakeDSS) handleProjects(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/public/api/projects/")

	f.mu.Lock()
	defer f.mu.Unlock()

	if rest == "" {
		switch r.Method {
		case http.MethodPost:
			var body map[string]any
			if !decodeBody(w, r, &body) {
				return
			}
			key, _ := body["projectKey"].(string)
			tags, _ := body["tags"].([]any)
			if tags == nil {
				tags = []any{}
			}
			f.projects[key] = map[string]any{
				"label":       body["name"],
				"description": orEmpty(body["description"]),
				"shortDesc":   "",
				"tags":        tags,
				// A field the provider does not model, to prove updates
				// preserve it.
				"checklists": map[string]any{"checklists": []any{}},
			}
			f.permissions[key] = map[string]any{"owner": body["owner"], "permissions": []any{}}
			f.variables[key] = map[string]any{"standard": map[string]any{}, "local": map[string]any{}}
			_, _ = w.Write([]byte(`"` + key + `"`))
		case http.MethodGet:
			out := []any{}
			for key, p := range f.projects {
				out = append(out, map[string]any{
					"projectKey": key,
					"label":      p["label"],
					"ownerLogin": f.permissions[key]["owner"],
					"shortDesc":  p["shortDesc"],
					"tags":       p["tags"],
				})
			}
			writeJSON(w, out)
		default:
			writeErr(w, http.StatusMethodNotAllowed, "bad method")
		}
		return
	}

	key, sub := splitFirst(strings.TrimSuffix(rest, "/"))
	if _, ok := f.projects[key]; !ok {
		writeErr(w, http.StatusNotFound, "No such project: "+key)
		return
	}

	switch {
	case sub == "metadata" && r.Method == http.MethodGet:
		writeJSON(w, f.projects[key])
	case sub == "metadata" && r.Method == http.MethodPut:
		var body map[string]any
		if !decodeBody(w, r, &body) {
			return
		}
		if _, ok := body["checklists"]; !ok {
			f.unmodelledFieldDropped = true
		}
		f.projects[key] = body
		w.WriteHeader(http.StatusOK)
	case sub == "permissions" && r.Method == http.MethodGet:
		writeJSON(w, f.permissions[key])
	case sub == "permissions" && r.Method == http.MethodPut:
		var body map[string]any
		if !decodeBody(w, r, &body) {
			return
		}
		f.permissions[key] = body
		w.WriteHeader(http.StatusOK)
	case sub == "variables" && r.Method == http.MethodGet:
		writeJSON(w, f.variables[key])
	case sub == "variables" && r.Method == http.MethodPut:
		var body map[string]any
		if !decodeBody(w, r, &body) {
			return
		}
		f.variables[key] = body
		w.WriteHeader(http.StatusOK)
	case strings.HasPrefix(sub, "scenarios"):
		f.handleScenarios(w, r, key, strings.TrimPrefix(strings.TrimPrefix(sub, "scenarios"), "/"))
	case sub == "" && r.Method == http.MethodDelete:
		delete(f.projects, key)
		delete(f.permissions, key)
		delete(f.variables, key)
		writeJSON(w, map[string]any{"hasResult": true})
	default:
		writeErr(w, http.StatusNotFound, "unhandled "+r.Method+" "+r.URL.Path)
	}
}

func (f *fakeDSS) handleGroups(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/public/api/admin/groups/")

	f.mu.Lock()
	defer f.mu.Unlock()

	if name == "" {
		switch r.Method {
		case http.MethodPost:
			var body map[string]any
			if !decodeBody(w, r, &body) {
				return
			}
			n, _ := body["name"].(string)
			body["admin"] = false
			body["mayCreateProjects"] = false
			// Real DSS returns these as arrays, not strings.
			body["ldapGroupNames"] = []any{}
			body["azureADGroupNames"] = []any{}
			// An ability this provider version does not model.
			body["mayUseSomeFutureFeature"] = true
			f.groups[n] = body
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			out := []any{}
			for _, g := range f.groups {
				out = append(out, g)
			}
			writeJSON(w, out)
		default:
			writeErr(w, http.StatusMethodNotAllowed, "bad method")
		}
		return
	}

	g, ok := f.groups[name]
	if !ok {
		writeErr(w, http.StatusNotFound, "No such group: "+name)
		return
	}

	switch r.Method {
	case http.MethodGet:
		writeJSON(w, g)
	case http.MethodPut:
		var body map[string]any
		if !decodeBody(w, r, &body) {
			return
		}
		if body["mayUseSomeFutureFeature"] != true {
			f.unmodelledFieldDropped = true
		}
		f.groups[name] = body
		w.WriteHeader(http.StatusOK)
	case http.MethodDelete:
		delete(f.groups, name)
		w.WriteHeader(http.StatusOK)
	default:
		writeErr(w, http.StatusMethodNotAllowed, "bad method")
	}
}

func (f *fakeDSS) handleUsers(w http.ResponseWriter, r *http.Request) {
	login := strings.TrimPrefix(r.URL.Path, "/public/api/admin/users/")

	f.mu.Lock()
	defer f.mu.Unlock()

	if login == "" {
		switch r.Method {
		case http.MethodPost:
			var body map[string]any
			if !decodeBody(w, r, &body) {
				return
			}
			l, _ := body["login"].(string)
			// DSS never returns the password.
			delete(body, "password")
			body["enabled"] = true
			body["userProperties"] = map[string]any{"team": "analytics"}
			f.users[l] = body
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			out := []any{}
			for _, u := range f.users {
				out = append(out, u)
			}
			writeJSON(w, out)
		default:
			writeErr(w, http.StatusMethodNotAllowed, "bad method")
		}
		return
	}

	u, ok := f.users[login]
	if !ok {
		writeErr(w, http.StatusNotFound, "No such user: "+login)
		return
	}

	switch r.Method {
	case http.MethodGet:
		writeJSON(w, u)
	case http.MethodPut:
		var body map[string]any
		if !decodeBody(w, r, &body) {
			return
		}
		if _, ok := body["userProperties"]; !ok {
			f.unmodelledFieldDropped = true
		}
		delete(body, "password")
		f.users[login] = body
		w.WriteHeader(http.StatusOK)
	case http.MethodDelete:
		delete(f.users, login)
		w.WriteHeader(http.StatusOK)
	default:
		writeErr(w, http.StatusMethodNotAllowed, "bad method")
	}
}

func (f *fakeDSS) handleConnections(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/public/api/admin/connections/")

	f.mu.Lock()
	defer f.mu.Unlock()

	if name == "" {
		switch r.Method {
		case http.MethodPost:
			var body map[string]any
			if !decodeBody(w, r, &body) {
				return
			}
			n, _ := body["name"].(string)
			f.connections[n] = body
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			writeJSON(w, f.connections)
		default:
			writeErr(w, http.StatusMethodNotAllowed, "bad method")
		}
		return
	}

	c, ok := f.connections[name]
	if !ok {
		writeErr(w, http.StatusNotFound, "No such connection: "+name)
		return
	}

	switch r.Method {
	case http.MethodGet:
		// DSS redacts secrets when reading a connection back.
		redacted := map[string]any{}
		for k, v := range c {
			redacted[k] = v
		}
		if params, ok := c["params"].(map[string]any); ok {
			copied := map[string]any{}
			for k, v := range params {
				if k == "password" {
					v = ""
				}
				copied[k] = v
			}
			redacted["params"] = copied
		}
		writeJSON(w, redacted)
	case http.MethodPut:
		var body map[string]any
		if !decodeBody(w, r, &body) {
			return
		}
		f.connections[name] = body
		w.WriteHeader(http.StatusOK)
	case http.MethodDelete:
		delete(f.connections, name)
		w.WriteHeader(http.StatusOK)
	default:
		writeErr(w, http.StatusMethodNotAllowed, "bad method")
	}
}

// storedConnectionPassword reports the password the instance actually holds,
// which is how the tests check that an update did not wipe a secret.
func (f *fakeDSS) storedConnectionPassword(name string) string {
	f.mu.Lock()
	defer f.mu.Unlock()

	conn, ok := f.connections[name]
	if !ok {
		return ""
	}
	params, ok := conn["params"].(map[string]any)
	if !ok {
		return ""
	}
	pwd, _ := params["password"].(string)
	return pwd
}

func (f *fakeDSS) projectCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.projects)
}

func (f *fakeDSS) droppedUnmodelledField() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.unmodelledFieldDropped
}

func splitFirst(s string) (string, string) {
	parts := strings.SplitN(s, "/", 2)
	if len(parts) == 1 {
		return parts[0], ""
	}
	return parts[0], parts[1]
}

func orEmpty(v any) any {
	if v == nil {
		return ""
	}
	return v
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"message": message})
}

func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, "malformed body: "+err.Error())
		return false
	}
	return true
}

// handleCodeEnvs mirrors how DSS stores a code environment: most settings live
// under "desc", and a few are mirrored at the top level.
func (f *fakeDSS) handleCodeEnvs(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/public/api/admin/code-envs/")

	f.mu.Lock()
	defer f.mu.Unlock()

	if rest == "" {
		out := []any{}
		for _, e := range f.codeEnvs {
			out = append(out, e)
		}
		writeJSON(w, out)
		return
	}

	lang, remainder := splitFirst(strings.TrimSuffix(rest, "/"))
	name, sub := splitFirst(remainder)
	key := lang + "/" + name

	if sub == "packages" && r.Method == http.MethodPost {
		if _, ok := f.codeEnvs[key]; !ok {
			writeErr(w, http.StatusNotFound, "No such code env: "+key)
			return
		}
		writeJSON(w, map[string]any{"messages": map[string]any{"error": false, "messages": []any{}}})
		return
	}

	switch r.Method {
	case http.MethodPost:
		var body map[string]any
		if !decodeBody(w, r, &body) {
			return
		}
		interpreter, _ := body["pythonInterpreter"].(string)
		if interpreter == "" {
			interpreter = "PYTHON39"
		}
		f.codeEnvs[key] = map[string]any{
			"envName":         name,
			"envLang":         lang,
			"deploymentMode":  body["deploymentMode"],
			"specPackageList": "",
			"usableByAll":     true,
			"desc": map[string]any{
				"pythonInterpreter":     interpreter,
				"conda":                 body["conda"] == true,
				"deploymentMode":        body["deploymentMode"],
				"installCorePackages":   false,
				"corePackagesSet":       "PANDAS23",
				"installJupyterSupport": false,
				"usableByAll":           true,
			},
		}
		writeJSON(w, map[string]any{"envName": name})
	case http.MethodGet:
		env, ok := f.codeEnvs[key]
		if !ok {
			writeErr(w, http.StatusNotFound, "No such code env: "+key)
			return
		}
		writeJSON(w, env)
	case http.MethodPut:
		if _, ok := f.codeEnvs[key]; !ok {
			writeErr(w, http.StatusNotFound, "No such code env: "+key)
			return
		}
		var body map[string]any
		if !decodeBody(w, r, &body) {
			return
		}
		f.codeEnvs[key] = body
		w.WriteHeader(http.StatusOK)
	case http.MethodDelete:
		delete(f.codeEnvs, key)
		writeJSON(w, map[string]any{"messages": map[string]any{"error": false}})
	default:
		writeErr(w, http.StatusMethodNotAllowed, "bad method")
	}
}

// scenarioIDFromName mirrors how DSS derives a scenario id: the name with
// anything outside [A-Za-z0-9_] replaced.
func scenarioIDFromName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}

// handleScenarios is called with the caller already holding f.mu.
func (f *fakeDSS) handleScenarios(w http.ResponseWriter, r *http.Request, projectKey, rest string) {
	id, sub := splitFirst(strings.TrimSuffix(rest, "/"))
	scenarioKey := projectKey + "/" + id

	if id == "" {
		switch r.Method {
		case http.MethodPost:
			var body map[string]any
			if !decodeBody(w, r, &body) {
				return
			}
			name, _ := body["name"].(string)
			newID := scenarioIDFromName(name)
			scenario := map[string]any{
				"projectKey": projectKey,
				"id":         newID,
				"name":       name,
				"type":       body["type"],
				"active":     false,
				"tags":       []any{},
				"triggers":   []any{},
				"reporters":  []any{},
				"params":     map[string]any{"steps": []any{}},
				// A field the provider does not model, to prove updates
				// preserve it.
				"checklists": map[string]any{"checklists": []any{}},
			}
			f.scenarios[projectKey+"/"+newID] = scenario
			writeJSON(w, map[string]any{"id": newID})
		case http.MethodGet:
			out := []any{}
			for k, s := range f.scenarios {
				if strings.HasPrefix(k, projectKey+"/") {
					out = append(out, s)
				}
			}
			writeJSON(w, out)
		default:
			writeErr(w, http.StatusMethodNotAllowed, "bad method")
		}
		return
	}

	scenario, ok := f.scenarios[scenarioKey]
	if !ok {
		writeErr(w, http.StatusNotFound, "No such scenario: "+id)
		return
	}

	if sub == "payload" {
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, map[string]any{"script": f.payloads[scenarioKey]})
		case http.MethodPut:
			var body map[string]any
			if !decodeBody(w, r, &body) {
				return
			}
			script, _ := body["script"].(string)
			f.payloads[scenarioKey] = script
			w.WriteHeader(http.StatusOK)
		default:
			writeErr(w, http.StatusMethodNotAllowed, "bad method")
		}
		return
	}

	switch r.Method {
	case http.MethodGet:
		writeJSON(w, scenario)
	case http.MethodPut:
		var body map[string]any
		if !decodeBody(w, r, &body) {
			return
		}
		if _, ok := body["checklists"]; !ok {
			f.unmodelledFieldDropped = true
		}
		f.scenarios[scenarioKey] = body
		w.WriteHeader(http.StatusOK)
	case http.MethodDelete:
		delete(f.scenarios, scenarioKey)
		delete(f.payloads, scenarioKey)
		w.WriteHeader(http.StatusOK)
	default:
		writeErr(w, http.StatusMethodNotAllowed, "bad method")
	}
}
