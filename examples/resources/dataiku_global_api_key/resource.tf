# A key for a deployment pipeline, carrying its own rights.
resource "dataiku_global_api_key" "ci" {
  label       = "ci"
  description = "Used by the deployment pipeline"

  # A new key is not empty-handed: DSS turns several abilities on by default,
  # mayCreateProjects among them. Name the ones you care about, in either
  # direction. The rest keep whatever the instance gave them.
  permissions = {
    mayCreateProjects  = true
    mayWriteUnsafeCode = false
  }
}

# A key that takes its rights from groups and expires after ninety days.
# The lifetime is counted from when the key is created.
resource "dataiku_global_api_key" "reporting" {
  label         = "reporting"
  groups        = ["readers"]
  lifetime_days = 90
}

# DSS shows the secret once, at creation, and this is the only place to read
# it afterwards. It is stored in state, so keep the state somewhere private.
output "ci_api_key" {
  value     = dataiku_global_api_key.ci.key
  sensitive = true
}
