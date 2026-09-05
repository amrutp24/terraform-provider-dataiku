package dataiku

import (
	"context"
	"net/url"
)

// ---------------------------------------------------------------------------
// Users
// ---------------------------------------------------------------------------

// UserListItem is one entry of GET /admin/users/.
type UserListItem struct {
	Login       string   `json:"login"`
	DisplayName string   `json:"displayName"`
	Email       string   `json:"email"`
	SourceType  string   `json:"sourceType"`
	UserProfile string   `json:"userProfile,omitempty"`
	Enabled     bool     `json:"enabled"`
	Groups      []string `json:"groups"`
}

// CreateUserRequest is the body of POST /admin/users/.
type CreateUserRequest struct {
	Login       string   `json:"login"`
	Password    string   `json:"password,omitempty"`
	DisplayName string   `json:"displayName"`
	SourceType  string   `json:"sourceType"`
	Groups      []string `json:"groups"`
	UserProfile string   `json:"userProfile,omitempty"`
	Email       string   `json:"email,omitempty"`
}

// ListUsers returns every user on the instance.
func (c *Client) ListUsers(ctx context.Context) ([]UserListItem, error) {
	var out []UserListItem
	if err := c.get(ctx, "/admin/users/", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CreateUser creates a user.
func (c *Client) CreateUser(ctx context.Context, req CreateUserRequest) error {
	if req.Groups == nil {
		req.Groups = []string{}
	}
	return c.post(ctx, "/admin/users/", nil, req, nil)
}

// GetUser returns a user's raw settings document.
func (c *Client) GetUser(ctx context.Context, login string) (map[string]any, error) {
	out := map[string]any{}
	if err := c.get(ctx, "/admin/users/"+url.PathEscape(login), nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// UpdateUser read-modify-writes a user so that fields this provider does not
// model (credentials, secrets, user properties) are preserved.
func (c *Client) UpdateUser(ctx context.Context, login string, mutate func(map[string]any)) error {
	current, err := c.GetUser(ctx, login)
	if err != nil {
		return err
	}
	mutate(current)
	return c.put(ctx, "/admin/users/"+url.PathEscape(login), nil, current, nil)
}

// DeleteUser removes a user.
func (c *Client) DeleteUser(ctx context.Context, login string) error {
	return c.delete(ctx, "/admin/users/"+url.PathEscape(login), nil)
}

// ---------------------------------------------------------------------------
// Groups
// ---------------------------------------------------------------------------

// GroupListItem is one entry of GET /admin/groups/.
type GroupListItem struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	SourceType  string `json:"sourceType"`
	Admin       bool   `json:"admin"`
}

// CreateGroupRequest is the body of POST /admin/groups/.
type CreateGroupRequest struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	SourceType  string `json:"sourceType"`
}

// ListGroups returns every group on the instance.
func (c *Client) ListGroups(ctx context.Context) ([]GroupListItem, error) {
	var out []GroupListItem
	if err := c.get(ctx, "/admin/groups/", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CreateGroup creates a group.
func (c *Client) CreateGroup(ctx context.Context, req CreateGroupRequest) error {
	return c.post(ctx, "/admin/groups/", nil, req, nil)
}

// GetGroup returns a group's raw definition.
func (c *Client) GetGroup(ctx context.Context, name string) (map[string]any, error) {
	out := map[string]any{}
	if err := c.get(ctx, "/admin/groups/"+url.PathEscape(name), nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// UpdateGroup read-modify-writes a group definition. DSS grows new
// "may<Something>" ability flags between releases; keeping the untouched keys
// means upgrading DSS never silently revokes an ability this provider version
// does not know about.
func (c *Client) UpdateGroup(ctx context.Context, name string, mutate func(map[string]any)) error {
	current, err := c.GetGroup(ctx, name)
	if err != nil {
		return err
	}
	mutate(current)
	return c.put(ctx, "/admin/groups/"+url.PathEscape(name), nil, current, nil)
}

// DeleteGroup removes a group.
func (c *Client) DeleteGroup(ctx context.Context, name string) error {
	return c.delete(ctx, "/admin/groups/"+url.PathEscape(name), nil)
}

// ---------------------------------------------------------------------------
// Connections
// ---------------------------------------------------------------------------

// CreateConnectionRequest is the body of POST /admin/connections/.
type CreateConnectionRequest struct {
	Name          string         `json:"name"`
	Type          string         `json:"type"`
	Description   string         `json:"description,omitempty"`
	Params        map[string]any `json:"params"`
	UsableBy      string         `json:"usableBy"`
	AllowedGroups []string       `json:"allowedGroups"`
}

// ListConnections returns every connection, keyed by connection name.
func (c *Client) ListConnections(ctx context.Context) (map[string]map[string]any, error) {
	out := map[string]map[string]any{}
	if err := c.get(ctx, "/admin/connections/", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CreateConnection creates a connection.
func (c *Client) CreateConnection(ctx context.Context, req CreateConnectionRequest) error {
	if req.Params == nil {
		req.Params = map[string]any{}
	}
	if req.AllowedGroups == nil {
		req.AllowedGroups = []string{}
	}
	return c.post(ctx, "/admin/connections/", nil, req, nil)
}

// GetConnection returns a connection's raw settings.
func (c *Client) GetConnection(ctx context.Context, name string) (map[string]any, error) {
	out := map[string]any{}
	if err := c.get(ctx, "/admin/connections/"+url.PathEscape(name), nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// UpdateConnection read-modify-writes a connection's settings.
func (c *Client) UpdateConnection(ctx context.Context, name string, mutate func(map[string]any)) error {
	current, err := c.GetConnection(ctx, name)
	if err != nil {
		return err
	}
	mutate(current)
	return c.put(ctx, "/admin/connections/"+url.PathEscape(name), nil, current, nil)
}

// DeleteConnection removes a connection.
func (c *Client) DeleteConnection(ctx context.Context, name string) error {
	return c.delete(ctx, "/admin/connections/"+url.PathEscape(name), nil)
}

// ---------------------------------------------------------------------------
// Code environments
// ---------------------------------------------------------------------------

// CodeEnvListItem is one entry of GET /admin/code-envs/.
type CodeEnvListItem struct {
	EnvName        string `json:"envName"`
	EnvLang        string `json:"envLang"`
	DeploymentMode string `json:"deploymentMode"`
	IsUpToDate     bool   `json:"isUptodate"`
}

// CreateCodeEnvRequest is the body of POST /admin/code-envs/{lang}/{name}.
type CreateCodeEnvRequest struct {
	DeploymentMode    string `json:"deploymentMode"`
	PythonInterpreter string `json:"pythonInterpreter,omitempty"`
	Conda             bool   `json:"conda,omitempty"`
}

func codeEnvPath(lang, name string) string {
	return "/admin/code-envs/" + url.PathEscape(lang) + "/" + url.PathEscape(name)
}

// ListCodeEnvs returns every code environment on the instance.
func (c *Client) ListCodeEnvs(ctx context.Context) ([]CodeEnvListItem, error) {
	var out []CodeEnvListItem
	if err := c.get(ctx, "/admin/code-envs/", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CreateCodeEnv creates a code environment. Creation only registers the
// environment; packages are installed by UpdateCodeEnvPackages.
func (c *Client) CreateCodeEnv(ctx context.Context, lang, name string, req CreateCodeEnvRequest) error {
	return c.post(ctx, codeEnvPath(lang, name), nil, req, nil)
}

// GetCodeEnv returns a code environment's raw settings document.
func (c *Client) GetCodeEnv(ctx context.Context, lang, name string) (map[string]any, error) {
	out := map[string]any{}
	if err := c.get(ctx, codeEnvPath(lang, name), nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// UpdateCodeEnv read-modify-writes a code environment's settings.
func (c *Client) UpdateCodeEnv(ctx context.Context, lang, name string, mutate func(map[string]any)) error {
	current, err := c.GetCodeEnv(ctx, lang, name)
	if err != nil {
		return err
	}
	mutate(current)
	return c.put(ctx, codeEnvPath(lang, name), nil, current, nil)
}

// UpdateCodeEnvPackages resolves and installs the environment's packages. This
// is the slow call: DSS runs pip or conda, so it needs network access on the
// instance and can take minutes.
func (c *Client) UpdateCodeEnvPackages(ctx context.Context, lang, name string) (map[string]any, error) {
	out := map[string]any{}
	if err := c.post(ctx, codeEnvPath(lang, name)+"/packages", nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// DeleteCodeEnv removes a code environment.
func (c *Client) DeleteCodeEnv(ctx context.Context, lang, name string) error {
	return c.delete(ctx, codeEnvPath(lang, name), nil)
}

// TestConnection asks DSS to actually dial the connection, rather than merely
// confirming the document parsed. Note the path is /connections/, not
// /admin/connections/.
//
// DSS answers with connectionOK=false for a reachable-but-misconfigured
// connection, and returns an error for connection types where testing is not
// supported at all.
func (c *Client) TestConnection(ctx context.Context, name string) (bool, error) {
	out := map[string]any{}
	if err := c.get(ctx, "/connections/"+url.PathEscape(name)+"/test", nil, &out); err != nil {
		return false, err
	}
	ok, _ := out["connectionOK"].(bool)
	return ok, nil
}
