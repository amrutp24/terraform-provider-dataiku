# Walk down from the top of the hierarchy.
data "dataiku_project_folder" "root" {
  id = "ROOT"
}

output "top_level_folders" {
  value = data.dataiku_project_folder.root.children_ids
}

# A folder's contents live on the data source rather than the resource: projects
# point at the folder, so a resource attribute would be read before they exist.
data "dataiku_project_folder" "analytics" {
  id = dataiku_project_folder.analytics.id
}

output "analytics_projects" {
  value = data.dataiku_project_folder.analytics.project_keys
}
