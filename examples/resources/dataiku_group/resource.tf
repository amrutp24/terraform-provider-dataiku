resource "dataiku_group" "data_scientists" {
  name        = "data_scientists"
  description = "Analysts who build and run models"

  # Ability names differ between DSS versions, so they are passed through by
  # their raw API field name. Read an existing group to discover the names
  # your instance supports:
  #
  #   data "dataiku_group" "reference" { name = "administrators" }
  #   output "abilities" { value = data.dataiku_group.reference.definition_json }
  #
  # Abilities left out of this map keep whatever value the instance has.
  permissions = {
    mayCreateProjects  = true
    mayWriteUnsafeCode = false
  }
}

resource "dataiku_group" "platform_admins" {
  name        = "platform_admins"
  description = "Full administrators of the instance"
  admin       = true
}
