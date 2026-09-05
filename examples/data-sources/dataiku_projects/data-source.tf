data "dataiku_projects" "all" {}

output "project_keys" {
  value = data.dataiku_projects.all.project_keys
}

# Apply the same standard variables to every project on the instance.
resource "dataiku_project_variables" "shared" {
  for_each = toset(data.dataiku_projects.all.project_keys)

  project_key = each.value
  standard    = jsonencode({ managed_by = "terraform" })
}
