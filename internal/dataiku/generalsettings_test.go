package dataiku

import "testing"

// DSS fills in defaults inside the blocks it is given, so a nested object is
// only ever a subset of what comes back. A field the caller did not mention
// appearing in the response is DSS doing its job, not a setting being lost.
func TestVerifyContainerConfigAppliedAllowsDSSToAddNestedDefaults(t *testing.T) {
	sent := map[string]any{
		"dockerBuilderConfig": map[string]any{
			"pushConfigs": []any{
				map[string]any{"repositoryURL": "us-central1-docker.pkg.dev/p/r", "prePushMode": "NONE"},
			},
		},
	}
	stored := map[string]any{
		"name":             "gke",
		"baseImageType":    "EXEC",
		"imageBuilderType": "DOCKER",
		"dockerBuilderConfig": map[string]any{
			// Not asked for; DSS chose it.
			"dockerTLSVerify": false,
			"pushConfigs": []any{
				map[string]any{"repositoryURL": "us-central1-docker.pkg.dev/p/r", "prePushMode": "NONE"},
			},
		},
	}

	if missing := VerifyContainerConfigApplied(sent, stored); len(missing) != 0 {
		t.Errorf("a nested default DSS added was reported as a lost setting: %v", missing)
	}
}

// The check still has to catch the thing it exists for: a value DSS quietly
// changed or dropped inside a block it was handed.
func TestVerifyContainerConfigAppliedCatchesNestedChanges(t *testing.T) {
	cases := map[string]struct {
		sent, stored map[string]any
	}{
		"nested value changed": {
			sent:   map[string]any{"k": map[string]any{"createNamespace": true}},
			stored: map[string]any{"k": map[string]any{"createNamespace": false}},
		},
		"nested key dropped": {
			sent:   map[string]any{"k": map[string]any{"kubeNamespace": "dssns"}},
			stored: map[string]any{"k": map[string]any{"other": 1}},
		},
		"top-level key dropped": {
			sent:   map[string]any{"type": "KUBERNETES"},
			stored: map[string]any{"name": "gke"},
		},
		"list gained an element": {
			sent:   map[string]any{"allowedGroups": []any{"a"}},
			stored: map[string]any{"allowedGroups": []any{"a", "b"}},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if missing := VerifyContainerConfigApplied(tc.sent, tc.stored); len(missing) == 0 {
				t.Error("a setting DSS did not keep was reported as applied")
			}
		})
	}
}
