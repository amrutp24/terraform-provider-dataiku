package dataiku

import (
	"context"
	"net/url"
)

// RootProjectFolderID is the id of the folder every other folder descends
// from. It always exists and cannot be created or deleted.
const RootProjectFolderID = "ROOT"

// ProjectFolder is the payload of /project-folders/{id}.
type ProjectFolder struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	ParentID    string   `json:"parentId"`
	Owner       string   `json:"owner"`
	ProjectKeys []string `json:"projectKeys"`
	ChildrenIDs []string `json:"childrenIds"`
}

// GetProjectFolder reads one folder.
func (c *Client) GetProjectFolder(ctx context.Context, id string) (*ProjectFolder, error) {
	out := &ProjectFolder{}
	if err := c.get(ctx, "/project-folders/"+url.PathEscape(id), nil, out); err != nil {
		return nil, err
	}
	return out, nil
}

// CreateProjectFolder creates a folder under parentID. DSS assigns the id and
// takes the name as a query parameter rather than in a body.
func (c *Client) CreateProjectFolder(ctx context.Context, parentID, name string) (*ProjectFolder, error) {
	if parentID == "" {
		parentID = RootProjectFolderID
	}
	query := url.Values{"name": []string{name}}
	out := &ProjectFolder{}
	path := "/project-folders/" + url.PathEscape(parentID) + "/children"
	if err := c.post(ctx, path, query, nil, out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetProjectFolderSettings returns a folder's raw settings document, which
// carries the name, owner and permissions.
func (c *Client) GetProjectFolderSettings(ctx context.Context, id string) (map[string]any, error) {
	out := map[string]any{}
	if err := c.get(ctx, "/project-folders/"+url.PathEscape(id)+"/settings", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// UpdateProjectFolderSettings read-modify-writes a folder's settings, so that
// permissions this provider does not model survive a rename.
func (c *Client) UpdateProjectFolderSettings(ctx context.Context, id string, mutate func(map[string]any)) error {
	current, err := c.GetProjectFolderSettings(ctx, id)
	if err != nil {
		return err
	}
	mutate(current)
	return c.put(ctx, "/project-folders/"+url.PathEscape(id)+"/settings", nil, current, nil)
}

// MoveProjectFolder reparents a folder.
func (c *Client) MoveProjectFolder(ctx context.Context, id, destinationID string) error {
	query := url.Values{"destination": []string{destinationID}}
	return c.post(ctx, "/project-folders/"+url.PathEscape(id)+"/move", query, nil, nil)
}

// DeleteProjectFolder removes a folder. DSS refuses while it still holds
// projects or child folders.
func (c *Client) DeleteProjectFolder(ctx context.Context, id string) error {
	return c.delete(ctx, "/project-folders/"+url.PathEscape(id), nil)
}
