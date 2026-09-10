# How DSS builds the container images that containerized execution runs, and
# where it pushes them. Building the base images themselves is a separate,
# host-side step: `dssadmin build-base-image --type container-exec` on the DSS
# machine, which needs a working Docker daemon there and must be repeated after
# every DSS upgrade.
resource "dataiku_container_image_build_config" "gke" {
  name = "gke-build"

  settings_json = jsonencode({
    type             = "KUBERNETES"
    baseImageType    = "EXEC"
    imageBuilderType = "DOCKER"
  })
}

# DSS 15 introduced an in-cluster builder for hosts with no Docker daemon,
# which builds inside the cluster instead. Its settings live in their own block,
# and which blocks a given DSS version reads is not documented: apply, then read
# `effective_json` to see what the instance actually kept.
resource "dataiku_container_image_build_config" "in_cluster" {
  name = "in-cluster-build"

  settings_json = jsonencode({
    type             = "KUBERNETES"
    baseImageType    = "EXEC"
    imageBuilderType = "DKU_IN_CLUSTER"
  })
}
