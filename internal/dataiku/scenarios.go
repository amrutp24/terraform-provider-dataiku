package dataiku

import (
	"context"
	"net/url"
)

// ScenarioListItem is one entry of GET /projects/{key}/scenarios/.
type ScenarioListItem struct {
	ID         string   `json:"id"`
	ProjectKey string   `json:"projectKey"`
	Name       string   `json:"name"`
	Type       string   `json:"type"`
	Active     bool     `json:"active"`
	Running    bool     `json:"running"`
	Tags       []string `json:"tags"`
}

func scenarioPath(projectKey, id string) string {
	return "/projects/" + url.PathEscape(projectKey) + "/scenarios/" + url.PathEscape(id)
}

// ListScenarios returns every scenario in a project.
func (c *Client) ListScenarios(ctx context.Context, projectKey string) ([]ScenarioListItem, error) {
	var out []ScenarioListItem
	path := "/projects/" + url.PathEscape(projectKey) + "/scenarios/"
	if err := c.get(ctx, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CreateScenario creates a scenario from a definition document and returns the
// id DSS assigned it. That id is derived from the name rather than random, so
// two scenarios created with the same name in one project will collide.
func (c *Client) CreateScenario(ctx context.Context, projectKey string, definition map[string]any) (string, error) {
	out := map[string]any{}
	path := "/projects/" + url.PathEscape(projectKey) + "/scenarios/"
	if err := c.post(ctx, path, nil, definition, &out); err != nil {
		return "", err
	}
	id, _ := out["id"].(string)
	return id, nil
}

// GetScenario returns a scenario's raw definition.
func (c *Client) GetScenario(ctx context.Context, projectKey, id string) (map[string]any, error) {
	out := map[string]any{}
	if err := c.get(ctx, scenarioPath(projectKey, id), nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// UpdateScenario read-modify-writes a scenario definition, so that the version
// tag, checklists and the other fields this provider does not model survive.
func (c *Client) UpdateScenario(ctx context.Context, projectKey, id string, mutate func(map[string]any)) error {
	current, err := c.GetScenario(ctx, projectKey, id)
	if err != nil {
		return err
	}
	mutate(current)
	return c.put(ctx, scenarioPath(projectKey, id), nil, current, nil)
}

// DeleteScenario removes a scenario.
func (c *Client) DeleteScenario(ctx context.Context, projectKey, id string) error {
	return c.delete(ctx, scenarioPath(projectKey, id), nil)
}

// GetScenarioPayload returns the Python script backing a custom_python
// scenario. It is empty for a step_based one.
func (c *Client) GetScenarioPayload(ctx context.Context, projectKey, id string) (string, error) {
	out := map[string]any{}
	if err := c.get(ctx, scenarioPath(projectKey, id)+"/payload", nil, &out); err != nil {
		return "", err
	}
	script, _ := out["script"].(string)
	return script, nil
}

// SetScenarioPayload replaces the Python script of a custom_python scenario.
func (c *Client) SetScenarioPayload(ctx context.Context, projectKey, id, script string) error {
	body := map[string]any{"script": script}
	return c.put(ctx, scenarioPath(projectKey, id)+"/payload", nil, body, nil)
}
