package dataiku

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
)

// ProjectListItem is one entry of GET /projects/.
type ProjectListItem struct {
	ProjectKey  string   `json:"projectKey"`
	Name        string   `json:"name"`
	Label       string   `json:"label"`
	Description string   `json:"description"`
	ShortDesc   string   `json:"shortDesc"`
	OwnerLogin  string   `json:"ownerLogin"`
	Tags        []string `json:"tags"`
}

// DisplayName returns the project's human-readable name, tolerating the two
// spellings DSS uses across endpoints.
func (p ProjectListItem) DisplayName() string {
	if p.Label != "" {
		return p.Label
	}
	return p.Name
}

// CreateProjectRequest is the body of POST /projects/.
type CreateProjectRequest struct {
	ProjectKey  string              `json:"projectKey"`
	Name        string              `json:"name"`
	Owner       string              `json:"owner"`
	Description string              `json:"description,omitempty"`
	Tags        []string            `json:"tags"`
	Permissions []ProjectPermission `json:"permissions"`

	// ProjectFolderID places the project in a folder. It is a query
	// parameter rather than a body field.
	ProjectFolderID string `json:"-"`
}

// ProjectPermission grants a set of rights on a project to one group.
type ProjectPermission struct {
	Group                          string `json:"group,omitempty"`
	User                           string `json:"user,omitempty"`
	Admin                          bool   `json:"admin,omitempty"`
	ReadProjectContent             bool   `json:"readProjectContent,omitempty"`
	WriteProjectContent            bool   `json:"writeProjectContent,omitempty"`
	ReadDashboards                 bool   `json:"readDashboards,omitempty"`
	WriteDashboards                bool   `json:"writeDashboards,omitempty"`
	MoveJob                        bool   `json:"moveJob,omitempty"`
	RunScenario                    bool   `json:"runScenario,omitempty"`
	ManageDashboardAuthorizations  bool   `json:"manageDashboardAuthorizations,omitempty"`
	ManageExposedElements          bool   `json:"manageExposedElements,omitempty"`
	ManageAdditionalDashboardUsers bool   `json:"manageAdditionalDashboardUsers,omitempty"`
	ExportDatasetsData             bool   `json:"exportDatasetsData,omitempty"`
	ShareToWorkspaces              bool   `json:"shareToWorkspaces,omitempty"`
}

// ProjectPermissions is the payload of /projects/{key}/permissions.
type ProjectPermissions struct {
	Owner       string              `json:"owner"`
	Permissions []ProjectPermission `json:"permissions"`
}

// ListProjects returns every project the API key may read.
func (c *Client) ListProjects(ctx context.Context) ([]ProjectListItem, error) {
	var out []ProjectListItem
	if err := c.get(ctx, "/projects/", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CreateProject creates a project. DSS answers with a bare string, so nothing
// is decoded from the response.
func (c *Client) CreateProject(ctx context.Context, req CreateProjectRequest) error {
	if req.Tags == nil {
		req.Tags = []string{}
	}
	if req.Permissions == nil {
		req.Permissions = []ProjectPermission{}
	}
	var query url.Values
	if req.ProjectFolderID != "" {
		query = url.Values{"projectFolderId": []string{req.ProjectFolderID}}
	}
	return c.post(ctx, "/projects/", query, req, nil)
}

// DeleteProjectOptions controls what DSS clears along with the project.
type DeleteProjectOptions struct {
	ClearManagedDatasets      bool
	ClearOutputManagedFolders bool
	ClearJobAndScenarioLogs   bool
}

// DeleteProject permanently deletes a project.
func (c *Client) DeleteProject(ctx context.Context, projectKey string, opts DeleteProjectOptions) error {
	query := url.Values{
		"clearManagedDatasets":      []string{strconv.FormatBool(opts.ClearManagedDatasets)},
		"clearOutputManagedFolders": []string{strconv.FormatBool(opts.ClearOutputManagedFolders)},
		"clearJobAndScenarioLogs":   []string{strconv.FormatBool(opts.ClearJobAndScenarioLogs)},
		"wait":                      []string{"true"},
	}
	return c.delete(ctx, "/projects/"+url.PathEscape(projectKey), query)
}

// GetProjectMetadata returns the raw metadata document. It is returned as a
// map so that fields this provider does not model survive a round trip.
func (c *Client) GetProjectMetadata(ctx context.Context, projectKey string) (map[string]any, error) {
	out := map[string]any{}
	if err := c.get(ctx, "/projects/"+url.PathEscape(projectKey)+"/metadata", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// UpdateProjectMetadata reads the current metadata, applies mutate to it and
// writes the whole document back. DSS replaces the document wholesale on PUT,
// so the read-modify-write is what keeps unmanaged fields (checklists, custom
// fields) intact.
func (c *Client) UpdateProjectMetadata(ctx context.Context, projectKey string, mutate func(map[string]any)) error {
	current, err := c.GetProjectMetadata(ctx, projectKey)
	if err != nil {
		return err
	}
	mutate(current)
	return c.put(ctx, "/projects/"+url.PathEscape(projectKey)+"/metadata", nil, current, nil)
}

// GetProjectPermissions returns the owner and per-group grants of a project.
func (c *Client) GetProjectPermissions(ctx context.Context, projectKey string) (*ProjectPermissions, error) {
	out := &ProjectPermissions{}
	if err := c.get(ctx, "/projects/"+url.PathEscape(projectKey)+"/permissions", nil, out); err != nil {
		return nil, err
	}
	return out, nil
}

// SetProjectPermissions replaces the owner and grants of a project.
func (c *Client) SetProjectPermissions(ctx context.Context, projectKey string, perms ProjectPermissions) error {
	if perms.Permissions == nil {
		perms.Permissions = []ProjectPermission{}
	}
	return c.put(ctx, "/projects/"+url.PathEscape(projectKey)+"/permissions", nil, perms, nil)
}

// ProjectVariables holds the two variable scopes DSS exposes. Standard
// variables are versioned with the project; local ones are per-instance.
type ProjectVariables struct {
	Standard map[string]any `json:"standard"`
	Local    map[string]any `json:"local"`
}

// GetProjectVariables reads a project's variables.
func (c *Client) GetProjectVariables(ctx context.Context, projectKey string) (*ProjectVariables, error) {
	out := &ProjectVariables{}
	if err := c.get(ctx, "/projects/"+url.PathEscape(projectKey)+"/variables/", nil, out); err != nil {
		return nil, err
	}
	if out.Standard == nil {
		out.Standard = map[string]any{}
	}
	if out.Local == nil {
		out.Local = map[string]any{}
	}
	return out, nil
}

// SetProjectVariables replaces a project's variables.
func (c *Client) SetProjectVariables(ctx context.Context, projectKey string, vars ProjectVariables) error {
	if vars.Standard == nil {
		vars.Standard = map[string]any{}
	}
	if vars.Local == nil {
		vars.Local = map[string]any{}
	}
	return c.put(ctx, "/projects/"+url.PathEscape(projectKey)+"/variables/", nil, vars, nil)
}

// ProjectExists reports whether a project key resolves on the instance.
func (c *Client) ProjectExists(ctx context.Context, projectKey string) (bool, error) {
	_, err := c.GetProjectMetadata(ctx, projectKey)
	if err == nil {
		return true, nil
	}
	if IsNotFound(err) {
		return false, nil
	}
	return false, fmt.Errorf("checking project %q: %w", projectKey, err)
}
