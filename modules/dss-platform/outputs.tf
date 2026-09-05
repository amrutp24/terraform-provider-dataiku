output "folder_id" {
  description = "Id of the project folder, when one was created. DSS assigns these, so it is not predictable in advance."
  value       = var.folder_name == null ? null : dataiku_project_folder.team[0].id
}

output "group_names" {
  description = "Names of the groups created."
  value       = sort([for group in dataiku_group.this : group.name])
}

output "code_env_ids" {
  description = "Code environments created, as \"<lang>/<name>\"."
  value       = sort([for env in dataiku_code_env.this : env.id])
}

output "project_keys" {
  description = "Keys of the projects created."
  value       = sort([for project in dataiku_project.this : project.project_key])
}
