package provider

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"time"
)

// Everything this handler does was measured against a DSS 15 instance first.
// None of it is what you would guess from the endpoint's shape, and each
// oddity here is one a provider written against a tidier fake would have got
// wrong:
//
//   - The secret comes back from the create call and never again. Reads and
//     the list return "******".
//   - PUT replaces the document. Whatever is not in the body is gone: the
//     description, createdOn, every ability back to its default.
//   - PUT ignores "key". The secret cannot be changed or chosen.
//   - PUT with no "id" in the body is a 500, and with the wrong one a 400.
//   - A missing key is 400 "Key not found", and on DELETE 400 "Key to delete
//     not found". Never 404.
//   - A group that does not exist is the one thing that is a 404.
//   - The presence of "groups" makes a key group-based. Such a key has no
//     globalPermissions, sent or not, and cannot be turned back.
//   - Ability names DSS does not know are dropped with a 200.
//   - The create response says expiresOn is 0 whatever the lifetime. A read
//     has the real date, which is createdOn plus expiry days.

// apiKeyDefaultAbilities is what a new key gets when it asks for nothing. Six
// of them are on, which is worth knowing before handing a key out.
var apiKeyDefaultAbilities = map[string]bool{
	"admin":                       false,
	"mayCreateProjects":           true,
	"mayWriteInRootProjectFolder": true,
	"mayCreateActiveWebContent":   true,
	"mayCreateDataCollections":    true,
	"mayPublishToDataCollections": true,
	"mayShareToWorkspaces":        true,
	"mayWriteSafeCode":            false,
	"mayWriteUnsafeCode":          false,
	"mayCreateCodeEnvs":           false,
	"mayManageCodeEnvs":           false,
	"mayCreateClusters":           false,
	"mayManageClusters":           false,
	"mayDevelopPlugins":           false,
	"mayEditLibFolders":           false,
	"mayManageUDM":                false,
}

// randomHex returns n characters nobody could guess. Real ids and secrets
// use letters and digits of both cases; nothing here depends on which.
func randomHex(n int) string {
	raw := make([]byte, (n+1)/2)
	_, _ = rand.Read(raw)
	return hex.EncodeToString(raw)[:n]
}

// authenticates reports whether secret is a key this instance would accept:
// the one the suite was started with, or a key created since and not deleted.
// The caller holds the lock.
func (f *fakeDSS) authenticates(secret string) bool {
	if secret == "acceptance-test-key" {
		return true
	}
	for _, live := range f.apiKeySecrets {
		if live == secret {
			return true
		}
	}
	return false
}

// storedAPIKey builds the document DSS keeps from the one it was sent. It is
// built from nothing each time, which is the point: a field that was not sent
// does not survive.
func (f *fakeDSS) storedAPIKey(id string, sent map[string]any) (map[string]any, int, string) {
	doc := map[string]any{"id": id}
	for _, field := range []string{"label", "description", "dssUserForImpersonation"} {
		if v, ok := sent[field].(string); ok {
			doc[field] = v
		}
	}
	expiry, _ := sent["expiry"].(float64)
	doc["expiry"] = expiry
	createdOn, _ := sent["createdOn"].(float64)
	doc["createdOn"] = createdOn

	if groups, groupBased := sent["groups"]; groupBased {
		names := asList(groups)
		for _, g := range names {
			name, _ := g.(string)
			if _, ok := f.groups[name]; !ok {
				return nil, http.StatusNotFound, "Group '" + name + "' doesn't exist"
			}
		}
		doc["groups"] = names
		return doc, 0, ""
	}

	abilities := map[string]any{}
	for name, value := range apiKeyDefaultAbilities {
		abilities[name] = value
	}
	if asked, ok := sent["globalPermissions"].(map[string]any); ok {
		for name, value := range asked {
			if _, known := apiKeyDefaultAbilities[name]; known {
				abilities[name] = value
			}
		}
	}
	doc["globalPermissions"] = abilities
	return doc, 0, ""
}

// shownAPIKey is a stored key as a read returns it: secret masked, expiry
// date worked out from the creation time.
func shownAPIKey(doc map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range doc {
		out[k] = v
	}
	out["key"] = "******"
	out["expiresOn"] = float64(0)
	if expiry, _ := doc["expiry"].(float64); expiry > 0 {
		createdOn, _ := doc["createdOn"].(float64)
		out["expiresOn"] = createdOn + expiry*86400000
	}
	return out
}

func (f *fakeDSS) handleGlobalAPIKeys(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/public/api/admin/global-api-keys/")

	f.mu.Lock()
	defer f.mu.Unlock()

	if id == "" {
		switch r.Method {
		case http.MethodPost:
			var body map[string]any
			if !decodeBody(w, r, &body) {
				return
			}
			newID := randomHex(16)
			body["createdOn"] = float64(time.Now().UnixMilli())
			doc, status, message := f.storedAPIKey(newID, body)
			if status != 0 {
				writeErr(w, status, message)
				return
			}
			secret := "dkuaps-" + randomHex(32)
			f.apiKeys[newID] = doc
			f.apiKeySecrets[newID] = secret

			out := shownAPIKey(doc)
			out["key"] = secret
			out["expiresOn"] = float64(0)
			writeJSON(w, out)
		case http.MethodGet:
			out := []any{}
			for _, doc := range f.apiKeys {
				out = append(out, shownAPIKey(doc))
			}
			writeJSON(w, out)
		default:
			writeErr(w, http.StatusMethodNotAllowed, "bad method")
		}
		return
	}

	current, exists := f.apiKeys[id]

	switch r.Method {
	case http.MethodGet:
		if !exists {
			writeErr(w, http.StatusBadRequest, "Key not found")
			return
		}
		writeJSON(w, shownAPIKey(current))
	case http.MethodPut:
		var body map[string]any
		if !decodeBody(w, r, &body) {
			return
		}
		bodyID, hasID := body["id"].(string)
		if !hasID {
			writeErr(w, http.StatusInternalServerError, "Internal error")
			return
		}
		if bodyID != id {
			writeErr(w, http.StatusBadRequest, "'id' mismatch URI vs Body")
			return
		}
		if !exists {
			writeErr(w, http.StatusBadRequest, "Key not found")
			return
		}
		if _, wasGroupBased := current["groups"]; wasGroupBased {
			if _, stillGroupBased := body["groups"]; !stillGroupBased {
				writeErr(w, http.StatusBadRequest,
					"Converting a global API key with groups into a global API key without groups is not allowed")
				return
			}
		}
		doc, status, message := f.storedAPIKey(id, body)
		if status != 0 {
			writeErr(w, status, message)
			return
		}
		f.apiKeys[id] = doc
		w.WriteHeader(http.StatusOK)
	case http.MethodDelete:
		if !exists {
			writeErr(w, http.StatusBadRequest, "Key to delete not found")
			return
		}
		delete(f.apiKeys, id)
		delete(f.apiKeySecrets, id)
		w.WriteHeader(http.StatusOK)
	default:
		writeErr(w, http.StatusMethodNotAllowed, "bad method")
	}
}

// apiKey returns what the fake holds for a key, for assertions about what the
// provider actually sent.
func (f *fakeDSS) apiKey(id string) (map[string]any, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	doc, ok := f.apiKeys[id]
	return doc, ok
}
