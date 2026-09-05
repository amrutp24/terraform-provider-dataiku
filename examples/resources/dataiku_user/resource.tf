resource "dataiku_user" "jsmith" {
  login        = "jsmith"
  display_name = "J. Smith"
  email        = "jsmith@example.com"
  user_profile = "FULL_DESIGNER"

  # Keep the initial password out of configuration; supply it with
  # TF_VAR_initial_password or from a secret manager.
  password = var.initial_password

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
