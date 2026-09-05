# A nightly build of the project's flow.
#
# Triggers and steps are JSON because their shape depends on the trigger and
# step type and shifts between DSS versions. The quickest way to get a
# definition worth editing is to build the scenario in the DSS interface, run
# `terraform import`, and copy what comes out.
resource "dataiku_scenario" "nightly" {
  project_key = dataiku_project.analytics.project_key
  name        = "Nightly build"
  active      = true
  tags        = ["managed-by-terraform"]

  triggers_json = jsonencode([{
    id     = "nightly"
    name   = "nightly"
    type   = "temporal"
    active = true
    params = {
      repeatFrequency = 1
      hour            = 3
      minute          = 0
      timezone        = "SERVER"
    }
  }])

  steps_json = jsonencode([{
    id   = "build"
    name = "Build the flow"
    type = "build_flowitem"
    params = {
      builds = [{
        type       = "DATASET"
        itemId     = "customers_prepared"
        projectKey = dataiku_project.analytics.project_key
      }]
    }
  }])
}

# A scenario driven by a script rather than steps.
resource "dataiku_scenario" "scripted" {
  project_key = dataiku_project.analytics.project_key
  name        = "Scripted"
  type        = "custom_python"

  script = <<-EOT
    # Available to a custom_python scenario as `scenario`.
    scenario.build_dataset("customers_prepared")
  EOT
}
