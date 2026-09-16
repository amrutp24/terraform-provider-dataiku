# Where recipes, notebooks and models run. The reference to the image build
# configuration is by name, and DSS rejects the whole settings document if the
# name does not exist, so read it off the resource rather than repeating the
# string: that also gives Terraform the ordering.
resource "dataiku_container_execution_config" "gke" {
  name = "gke-exec"

  settings_json = jsonencode({
    type             = "KUBERNETES"
    imageBuildConfig = dataiku_container_image_build_config.gke.name
    usableBy         = "ALL"
    allowedGroups    = []
  })
}

# Namespace and resource settings go inside kubernetesRuntimeConfig. Putting
# them at the top level is the mistake worth knowing about: DSS answers 200,
# stores nothing, and the setting is simply absent. This resource reads every
# key back after writing and fails the apply if one did not survive, so that
# mistake is loud rather than silent.
resource "dataiku_container_execution_config" "team" {
  name = "team-exec"

  settings_json = jsonencode({
    type             = "KUBERNETES"
    imageBuildConfig = dataiku_container_image_build_config.gke.name
    workloadType     = "ANY"

    # Restrict who may select this configuration.
    usableBy      = "ALLOWED"
    allowedGroups = [dataiku_group.analysts.name]

    kubernetesRuntimeConfig = {
      # Dataiku suggests a namespace per user so quotas can be applied per
      # person. The field is kubernetesNamespace and it belongs here, inside
      # kubernetesRuntimeConfig: at the top level DSS drops it and every pod
      # lands in "default". The variable is expanded by DSS at run time, so
      # it is escaped here and Terraform emits it literally.
      kubernetesNamespace = "dssns-$${dssUserLogin}"
      createNamespace     = true

      kubernetesResources = {
        memRequestMB = 4096
        memLimitMB   = 8192
        cpuRequest   = 1
        cpuLimit     = 2

        # A GPU is requested as a custom limit, which is the one resource key
        # Dataiku's documentation actually names.
        customLimits = [
          { key = "nvidia.com/gpu", value = "1" },
        ]
      }
    }
  })
}

resource "dataiku_group" "analysts" {
  name = "analysts"
}
