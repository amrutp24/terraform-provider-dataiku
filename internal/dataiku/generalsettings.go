package dataiku

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// jsonEqual compares two decoded JSON values. Marshalling rather than using
// reflect.DeepEqual keeps map ordering out of it, and both sides have been
// through a JSON decoder already so their numbers are the same kind.
func jsonEqual(a, b any) bool {
	left, errA := json.Marshal(a)
	right, errB := json.Marshal(b)
	if errA != nil || errB != nil {
		return false
	}
	return string(left) == string(right)
}

// compact renders a value for an error message, short enough to read.
func compact(v any) string {
	encoded, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return truncate(string(encoded), 120)
}

// generalSettingsMu serialises read-modify-write cycles on the instance's
// general settings.
//
// Containerized execution lives inside one large document, so several
// resources in the same configuration all rewrite the same object. Terraform
// applies resources concurrently, and two overlapping cycles would each read
// the document before either wrote, so the second write would silently discard
// the first. The lock is per process rather than per instance, which is enough:
// a provider run holds one client, and DSS offers no compare-and-swap to make
// this safe between machines.
var generalSettingsMu sync.Mutex

// GeneralSettings returns the instance's general settings document.
//
// The document is large and largely undocumented. Dataiku's REST reference
// says only "You must only PUT an object that you acquired previously via the
// corresponding GET call", so callers must not construct one from scratch.
func (c *Client) GeneralSettings(ctx context.Context) (map[string]any, error) {
	out := map[string]any{}
	if err := c.get(ctx, "/admin/general-settings/", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// UpdateGeneralSettings read-modify-writes the general settings document.
//
// mutate receives the live document and edits it in place. Everything it does
// not touch is written back unchanged, which is what keeps this from wiping
// settings the provider does not model.
func (c *Client) UpdateGeneralSettings(ctx context.Context, mutate func(map[string]any) error) error {
	generalSettingsMu.Lock()
	defer generalSettingsMu.Unlock()

	current, err := c.GeneralSettings(ctx)
	if err != nil {
		return err
	}
	if err := mutate(current); err != nil {
		return err
	}
	return c.put(ctx, "/admin/general-settings/", nil, current, nil)
}

// ContainerConfigKind names one of the two lists under containerSettings.
type ContainerConfigKind string

const (
	// ImageBuildConfigs builds and pushes the images execution configs run.
	ImageBuildConfigs ContainerConfigKind = "buildConfigs"
	// ExecutionConfigs describes where and how containers run.
	ExecutionConfigs ContainerConfigKind = "executionConfigs"
)

// containerConfigs returns one of the containerSettings lists, creating the
// intermediate objects if the instance has never had any.
func containerConfigs(settings map[string]any, kind ContainerConfigKind) []any {
	container, ok := settings["containerSettings"].(map[string]any)
	if !ok {
		return nil
	}
	list, _ := container[string(kind)].([]any)
	return list
}

// FindContainerConfig returns the entry with the given name, or nil.
func (c *Client) FindContainerConfig(ctx context.Context, kind ContainerConfigKind, name string) (map[string]any, error) {
	settings, err := c.GeneralSettings(ctx)
	if err != nil {
		return nil, err
	}
	for _, entry := range containerConfigs(settings, kind) {
		item, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		if item["name"] == name {
			return item, nil
		}
	}
	return nil, nil
}

// PutContainerConfig inserts or replaces one entry by name.
//
// DSS rewrites what it is given: it fills in defaults, moves fields into nested
// runtime blocks, and drops anything it does not recognise at the level it was
// supplied, all while answering 200. Callers are expected to read the entry
// back and check that what they asked for survived; see
// VerifyContainerConfigApplied.
func (c *Client) PutContainerConfig(ctx context.Context, kind ContainerConfigKind, name string, entry map[string]any) error {
	return c.UpdateGeneralSettings(ctx, func(settings map[string]any) error {
		container, ok := settings["containerSettings"].(map[string]any)
		if !ok {
			container = map[string]any{}
			settings["containerSettings"] = container
		}

		list, _ := container[string(kind)].([]any)
		entry["name"] = name

		replaced := false
		for i, existing := range list {
			item, ok := existing.(map[string]any)
			if ok && item["name"] == name {
				list[i] = entry
				replaced = true
				break
			}
		}
		if !replaced {
			list = append(list, entry)
		}

		container[string(kind)] = list
		return nil
	})
}

// DeleteContainerConfig removes one entry by name. Removing something that is
// not there is not an error, so a destroy stays idempotent.
func (c *Client) DeleteContainerConfig(ctx context.Context, kind ContainerConfigKind, name string) error {
	return c.UpdateGeneralSettings(ctx, func(settings map[string]any) error {
		container, ok := settings["containerSettings"].(map[string]any)
		if !ok {
			return nil
		}
		list, _ := container[string(kind)].([]any)

		kept := make([]any, 0, len(list))
		for _, existing := range list {
			item, ok := existing.(map[string]any)
			if ok && item["name"] == name {
				continue
			}
			kept = append(kept, existing)
		}
		container[string(kind)] = kept
		return nil
	})
}

// VerifyContainerConfigApplied reports the fields that did not survive a write.
//
// This exists because DSS accepts a configuration it only partly applies. Send
// createNamespace at the top level of an execution config rather than inside
// kubernetesRuntimeConfig and it answers 200, stores nothing, and leaves the
// setting off. Without this check Terraform would report success for a
// configuration the instance is not running.
//
// Only the keys the caller supplied are compared. DSS adds a great many of its
// own, and those are expected.
func VerifyContainerConfigApplied(wanted, stored map[string]any) []string {
	var missing []string
	for key, want := range wanted {
		got, present := stored[key]
		if !present {
			missing = append(missing, fmt.Sprintf("%s (dropped)", key))
			continue
		}
		if !storedAsSent(want, got) {
			missing = append(missing, fmt.Sprintf("%s (sent %v, stored %v)", key, compact(want), compact(got)))
		}
	}
	return missing
}

// storedAsSent reports whether DSS kept what was sent, allowing it to have
// added to nested objects.
//
// The comparison has to descend rather than compare whole values, because DSS
// fills in defaults inside the blocks it is given. Ask for
//
//	dockerBuilderConfig: {pushConfigs: [...]}
//
// and it stores that alongside a dockerTLSVerify it chose itself. Comparing the
// two objects whole reports a difference on every apply for a configuration DSS
// accepted exactly as asked.
//
// Lists are compared whole on purpose. A list the caller wrote is the complete
// intended contents of that field, so an extra element is a real difference,
// not a default being filled in.
func storedAsSent(want, got any) bool {
	wantMap, wantIsMap := want.(map[string]any)
	gotMap, gotIsMap := got.(map[string]any)
	if wantIsMap && gotIsMap {
		for key, w := range wantMap {
			g, present := gotMap[key]
			if !present || !storedAsSent(w, g) {
				return false
			}
		}
		return true
	}
	return jsonEqual(want, got)
}
