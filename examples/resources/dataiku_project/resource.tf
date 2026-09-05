resource "dataiku_project" "analytics" {
  project_key = "ANALYTICS"
  name        = "Analytics"
  owner       = "admin"

  short_desc  = "Shared analytics workspace"
  description = "Datasets and recipes backing the weekly revenue report."

  tags = ["managed-by-terraform", "analytics"]
}

# Deleting a project is destructive. These arguments control how much DSS
# clears along with it; all default to keeping data except the logs.
resource "dataiku_project" "scratch" {
  project_key = "SCRATCH"
  name        = "Scratch"
  owner       = "admin"

  clear_managed_datasets_on_delete       = true
  clear_output_managed_folders_on_delete = true
}
