package provider

import (
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/amrutp24/terraform-provider-dataiku/internal/dataiku"
)

var (
	_ resource.Resource                = (*containerConfigResource)(nil)
	_ resource.ResourceWithConfigure   = (*containerConfigResource)(nil)
	_ resource.ResourceWithImportState = (*containerConfigResource)(nil)
)

// NewContainerImageBuildConfigResource returns
// dataiku_container_image_build_config.
func NewContainerImageBuildConfigResource() resource.Resource {
	return &containerConfigResource{
		kind:       dataiku.ImageBuildConfigs,
		typeSuffix: "container_image_build_config",
		description: "An image build configuration on a Dataiku DSS instance: how DSS builds the " +
			"container images that containerized execution runs, and which registry it pushes them to.\n\n" +
			"Building the base images themselves is not part of this resource and cannot be. It runs " +
			"`dssadmin build-base-image --type container-exec` on the DSS host, needs a working Docker " +
			"daemon there, and pushes multi-gigabyte images. Do that from the instance's bootstrap or a " +
			"provisioner, and note that **base images must be rebuilt manually after every DSS upgrade**.",
		settingsDesc: "The build configuration, as a JSON object. `baseImageType` and `imageBuilderType` " +
			"are the fields a DSS 15 instance expects alongside the builder-specific blocks " +
			"(`dockerBuilderConfig`, `dkuInClusterBuilderConfig`, `openshiftBuilderConfig`).",
	}
}

// NewContainerExecutionConfigResource returns
// dataiku_container_execution_config.
func NewContainerExecutionConfigResource() resource.Resource {
	return &containerConfigResource{
		kind:       dataiku.ExecutionConfigs,
		typeSuffix: "container_execution_config",
		description: "A containerized execution configuration on a Dataiku DSS instance: where " +
			"recipes, notebooks and models run, and under which image build configuration.\n\n" +
			"Reference an image build configuration by name through the `imageBuildConfig` key of " +
			"`settings_json`. DSS validates that the name exists and rejects the whole settings " +
			"document if it does not, so create the build configuration first; referring to a " +
			"`dataiku_container_image_build_config` resource's `name` gives Terraform the ordering.\n\n" +
			"Changing which image build configuration this points at **does not rebuild the code " +
			"environment images** that were built against the old one. Dataiku documents that " +
			"explicitly, so plan a code env rebuild alongside such a change.",
		settingsDesc: "The execution configuration, as a JSON object. A DSS 15 instance recognises " +
			"`type` (`KUBERNETES`), `imageBuildConfig`, `usableBy`, `allowedGroups`, `workloadType`, " +
			"and nests the rest under `kubernetesRuntimeConfig` and `dockerRuntimeConfig`. Namespace " +
			"and resource settings belong inside `kubernetesRuntimeConfig`, not at the top level: " +
			"supplied at the top level DSS drops them and still answers 200, which this resource " +
			"turns into a failed apply rather than a silent no-op.",
	}
}
