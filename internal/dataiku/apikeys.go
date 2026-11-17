package dataiku

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

// ---------------------------------------------------------------------------
// Global API keys
// ---------------------------------------------------------------------------
//
// Everything below was measured against DSS 15, because very little of it is
// what you would guess:
//
//   - The secret is returned once, by the create call. Every later read
//     returns "******" in its place.
//   - A PUT replaces the document. Send only an id and a label and DSS resets
//     every ability to its default, clears the description and zeroes
//     createdOn. So updates read the document and write all of it back.
//   - A PUT cannot change the secret. A different value in "key" is ignored.
//   - A key that does not exist answers 400 "Key not found", not 404.

const globalAPIKeysPath = "/admin/global-api-keys/"

// ListGlobalAPIKeys returns every global API key on the instance. Secrets are
// masked.
func (c *Client) ListGlobalAPIKeys(ctx context.Context) ([]map[string]any, error) {
	var out []map[string]any
	if err := c.get(ctx, globalAPIKeysPath, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CreateGlobalAPIKey creates a key and returns the document DSS answers with.
// This is the only response that carries the secret, under "key".
func (c *Client) CreateGlobalAPIKey(ctx context.Context, body map[string]any) (map[string]any, error) {
	out := map[string]any{}
	if err := c.post(ctx, globalAPIKeysPath, nil, body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetGlobalAPIKey returns a key's definition by id. The secret is masked.
func (c *Client) GetGlobalAPIKey(ctx context.Context, id string) (map[string]any, error) {
	out := map[string]any{}
	if err := c.get(ctx, globalAPIKeysPath+url.PathEscape(id), nil, &out); err != nil {
		return nil, apiKeyNotFound(err)
	}
	return out, nil
}

// UpdateGlobalAPIKey read-modify-writes a key's definition. The whole
// document goes back because DSS keeps nothing that is not in the PUT.
func (c *Client) UpdateGlobalAPIKey(ctx context.Context, id string, mutate func(map[string]any)) error {
	current, err := c.GetGlobalAPIKey(ctx, id)
	if err != nil {
		return err
	}
	// The read returns a mask here, and DSS ignores whatever is sent. Leaving
	// it out keeps a secret-shaped field off the wire altogether.
	delete(current, "key")
	mutate(current)
	return apiKeyNotFound(c.put(ctx, globalAPIKeysPath+url.PathEscape(id), nil, current, nil))
}

// DeleteGlobalAPIKey revokes a key. It stops authenticating at once.
func (c *Client) DeleteGlobalAPIKey(ctx context.Context, id string) error {
	return apiKeyNotFound(c.delete(ctx, globalAPIKeysPath+url.PathEscape(id), nil))
}

// apiKeyNotFound marks the 400 DSS sends for a missing key so IsNotFound
// recognises it. The messages are "Key not found" on GET and PUT and "Key to
// delete not found" on DELETE.
func apiKeyNotFound(err error) error {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return err
	}
	if apiErr.StatusCode == http.StatusBadRequest && strings.Contains(strings.ToLower(apiErr.Message), "not found") {
		apiErr.notFound = true
	}
	return err
}
