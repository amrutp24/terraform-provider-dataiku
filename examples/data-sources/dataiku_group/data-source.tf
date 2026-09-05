data "dataiku_group" "administrators" {
  name = "administrators"
}

# definition_json is the quickest way to discover which ability field names
# your DSS version supports before setting them on a dataiku_group resource.
output "administrator_abilities" {
  value = data.dataiku_group.administrators.definition_json
}
