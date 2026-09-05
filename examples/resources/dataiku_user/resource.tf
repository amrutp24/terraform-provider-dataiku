resource "dataiku_user" "jsmith" {
  login        = "jsmith"
  display_name = "J. Smith"
  email        = "jsmith@example.com"
  user_profile = "FULL_DESIGNER"

  # password_wo is never written to plan or state, unlike password. Bump the
  # version marker to rotate: the provider keeps nothing to compare against, so
  # changing the secret alone would go unnoticed. Needs Terraform 1.11+.
  password_wo         = var.initial_password
  password_wo_version = "1"

  groups = [dataiku_group.data_scientists.name]
}

# An LDAP-backed user has no password; DSS authenticates it against the
# directory instead.
resource "dataiku_user" "contractor" {
  login        = "contractor"
  display_name = "External Contractor"
  source_type  = "LDAP"
  user_profile = "AI_CONSUMER"
  enabled      = false
}
