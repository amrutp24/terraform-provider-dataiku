# The setting to reach for first. An execution configuration can be entirely
# correct and every containerized activity will still fail before it starts
# with "No default Kubernetes cluster selected" until DSS is told which cluster
# to use. With no cluster object registered, this is the one that does it: use
# whatever cluster kubectl on the DSS host is pointed at, which is what a
# gcloud container clusters get-credentials at boot leaves behind.
#
# There is one of these per instance, so declare it once. A second resource of
# this type manages the same fields and the two overwrite each other.
resource "dataiku_container_settings" "instance" {
  use_implicit_k8s_cluster = true

  # Where code recipes and notebooks run when the project names no
  # configuration of its own. Read the name off the resource so that Terraform
  # creates the configuration first.
  default_execution_config = dataiku_container_execution_config.gke.name

  # The other two defaults are left out, so whatever the instance holds for
  # them stays. The corollary is that removing an argument later does not put
  # its field back: set it to the value you want instead.
}

resource "dataiku_container_execution_config" "gke" {
  name = "gke-exec"

  settings_json = jsonencode({
    type             = "KUBERNETES"
    imageBuildConfig = dataiku_container_image_build_config.gke.name
    usableBy         = "ALL"
    allowedGroups    = []
  })
}

resource "dataiku_container_image_build_config" "gke" {
  name = "gke-build"

  settings_json = jsonencode({
    baseImageType    = "EXEC"
    imageBuilderType = "DOCKER"
  })
}
