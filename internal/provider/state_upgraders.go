package provider

import (
	"context"
	"maps"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// State written by provider versions up to 0.3.0 stores tags as a list. Schema
// version 1 makes them a set, because DSS returns tags in its own order and a
// list attribute turns that into
//
//	Provider produced inconsistent result after apply
//	.tags[0]: was cty.StringVal("terraform"), but now cty.StringVal("smoke-test")
//
// Terraform will not read state stamped with an older schema version unless the
// provider offers an upgrader, so without these anyone who applied with 0.3.0
// could not plan with 0.4.0 at all.

// priorSchemaWithListTags returns a resource's current schema with tags put
// back to a list. Deriving it from the live schema rather than restating it
// keeps the two from drifting apart as the resource grows attributes: only the
// one attribute that actually changed is described here.
func priorSchemaWithListTags(current schema.Schema) schema.Schema {
	attributes := make(map[string]schema.Attribute, len(current.Attributes))
	maps.Copy(attributes, current.Attributes)
	attributes["tags"] = schema.ListAttribute{
		Optional:    true,
		Computed:    true,
		ElementType: types.StringType,
	}

	current.Attributes = attributes
	current.Version = 0
	return current
}

type projectResourceModelV0 struct {
	ID              types.String `tfsdk:"id"`
	ProjectKey      types.String `tfsdk:"project_key"`
	Name            types.String `tfsdk:"name"`
	Owner           types.String `tfsdk:"owner"`
	Description     types.String `tfsdk:"description"`
	ShortDesc       types.String `tfsdk:"short_desc"`
	Tags            types.List   `tfsdk:"tags"`
	ProjectFolderID types.String `tfsdk:"project_folder_id"`

	ClearManagedDatasetsOnDelete      types.Bool `tfsdk:"clear_managed_datasets_on_delete"`
	ClearOutputManagedFoldersOnDelete types.Bool `tfsdk:"clear_output_managed_folders_on_delete"`
	ClearJobAndScenarioLogsOnDelete   types.Bool `tfsdk:"clear_job_and_scenario_logs_on_delete"`
}

func (r *projectResource) UpgradeState(ctx context.Context) map[int64]resource.StateUpgrader {
	var current resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &current)
	prior := priorSchemaWithListTags(current.Schema)

	return map[int64]resource.StateUpgrader{
		0: {
			PriorSchema: &prior,
			StateUpgrader: func(ctx context.Context, req resource.UpgradeStateRequest, resp *resource.UpgradeStateResponse) {
				var old projectResourceModelV0
				resp.Diagnostics.Append(req.State.Get(ctx, &old)...)
				if resp.Diagnostics.HasError() {
					return
				}

				tags := toStringSet(ctx, fromStringList(ctx, old.Tags, &resp.Diagnostics), &resp.Diagnostics)
				if resp.Diagnostics.HasError() {
					return
				}

				resp.Diagnostics.Append(resp.State.Set(ctx, projectResourceModel{
					ID:                                old.ID,
					ProjectKey:                        old.ProjectKey,
					Name:                              old.Name,
					Owner:                             old.Owner,
					Description:                       old.Description,
					ShortDesc:                         old.ShortDesc,
					Tags:                              tags,
					ProjectFolderID:                   old.ProjectFolderID,
					ClearManagedDatasetsOnDelete:      old.ClearManagedDatasetsOnDelete,
					ClearOutputManagedFoldersOnDelete: old.ClearOutputManagedFoldersOnDelete,
					ClearJobAndScenarioLogsOnDelete:   old.ClearJobAndScenarioLogsOnDelete,
				})...)
			},
		},
	}
}

type scenarioResourceModelV0 struct {
	ID            types.String         `tfsdk:"id"`
	ScenarioID    types.String         `tfsdk:"scenario_id"`
	ProjectKey    types.String         `tfsdk:"project_key"`
	Name          types.String         `tfsdk:"name"`
	Type          types.String         `tfsdk:"type"`
	Active        types.Bool           `tfsdk:"active"`
	Tags          types.List           `tfsdk:"tags"`
	TriggersJSON  jsontypes.Normalized `tfsdk:"triggers_json"`
	StepsJSON     jsontypes.Normalized `tfsdk:"steps_json"`
	ReportersJSON jsontypes.Normalized `tfsdk:"reporters_json"`
	Script        types.String         `tfsdk:"script"`
}

func (r *scenarioResource) UpgradeState(ctx context.Context) map[int64]resource.StateUpgrader {
	var current resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &current)
	prior := priorSchemaWithListTags(current.Schema)

	return map[int64]resource.StateUpgrader{
		0: {
			PriorSchema: &prior,
			StateUpgrader: func(ctx context.Context, req resource.UpgradeStateRequest, resp *resource.UpgradeStateResponse) {
				var old scenarioResourceModelV0
				resp.Diagnostics.Append(req.State.Get(ctx, &old)...)
				if resp.Diagnostics.HasError() {
					return
				}

				tags := toStringSet(ctx, fromStringList(ctx, old.Tags, &resp.Diagnostics), &resp.Diagnostics)
				if resp.Diagnostics.HasError() {
					return
				}

				resp.Diagnostics.Append(resp.State.Set(ctx, scenarioResourceModel{
					ID:            old.ID,
					ScenarioID:    old.ScenarioID,
					ProjectKey:    old.ProjectKey,
					Name:          old.Name,
					Type:          old.Type,
					Active:        old.Active,
					Tags:          tags,
					TriggersJSON:  old.TriggersJSON,
					StepsJSON:     old.StepsJSON,
					ReportersJSON: old.ReportersJSON,
					Script:        old.Script,
				})...)
			},
		},
	}
}
