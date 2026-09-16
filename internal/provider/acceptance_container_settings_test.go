package provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// TestAccContainerSettings covers the instance-wide defaults, including the
// flag without which every containerized activity fails before it starts.
func TestAccContainerSettings(t *testing.T) {
	testAccSetup(t)
	build := randName(t, "build")
	exec := randName(t, "exec")

	configs := fmt.Sprintf(`
resource "dataiku_container_image_build_config" "build" {
  name = %[1]q

  settings_json = jsonencode({
    baseImageType    = "EXEC"
    imageBuilderType = "DOCKER"
  })
}

resource "dataiku_container_execution_config" "exec" {
  name = %[2]q

  settings_json = jsonencode({
    type             = "KUBERNETES"
    imageBuildConfig = dataiku_container_image_build_config.build.name
    usableBy         = "ALL"
    allowedGroups    = []
  })
}
`, build, exec)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: configs + `
resource "dataiku_container_settings" "test" {
  use_implicit_k8s_cluster = true
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("dataiku_container_settings.test", "id", "container_settings"),
					resource.TestCheckResourceAttr("dataiku_container_settings.test", "use_implicit_k8s_cluster", "true"),
					// Absent is how DSS spells "nothing selected", and it has
					// to read back as null rather than as the empty string, or
					// every plan would show a change.
					resource.TestCheckNoResourceAttr("dataiku_container_settings.test", "default_execution_config"),
					resource.TestCheckNoResourceAttr("dataiku_container_settings.test", "default_k8s_cluster_id"),
				),
			},
			{
				// Naming an execution config for user code, which is the field
				// that decides where a code recipe runs.
				Config: configs + `
resource "dataiku_container_settings" "test" {
  use_implicit_k8s_cluster = true
  default_execution_config = dataiku_container_execution_config.exec.name
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("dataiku_container_settings.test", "use_implicit_k8s_cluster", "true"),
					resource.TestCheckResourceAttr("dataiku_container_settings.test", "default_execution_config", exec),
				),
			},
			{
				// Back off again, in place.
				Config: configs + `
resource "dataiku_container_settings" "test" {
  use_implicit_k8s_cluster = false
  default_execution_config = dataiku_container_execution_config.exec.name
}
`,
				Check: resource.TestCheckResourceAttr(
					"dataiku_container_settings.test", "use_implicit_k8s_cluster", "false"),
			},
			{
				ResourceName:      "dataiku_container_settings.test",
				ImportState:       true,
				ImportStateId:     "container_settings",
				ImportStateVerify: true,
			},
		},
	})
}

// TestAccContainerSettingsRestoresWhatItReplaced checks the destroy path.
//
// Nothing is created on the instance by this resource: the settings exist
// whether or not Terraform manages them, so a destroy that left them at
// Terraform's values would quietly keep a change nobody asked to keep.
func TestAccContainerSettingsRestoresWhatItReplaced(t *testing.T) {
	fake := testAccSetup(t)
	if fake == nil {
		t.Skip("runs against the fake only: it asserts on the instance-wide document afterwards")
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "dataiku_container_settings" "test" {
  use_implicit_k8s_cluster = true
}
`,
			},
			{
				// A second write, so that the destroy below happens after an
				// update. What the fields held before Terraform took them over
				// is recorded once, at create; an update that loses that record
				// would restore this step's values instead of the original
				// ones, and the check would not notice with a single step.
				Config: `
resource "dataiku_container_settings" "test" {
  use_implicit_k8s_cluster = true
  default_k8s_cluster_id   = "some-cluster"
}
`,
				Check: resource.TestCheckResourceAttr(
					"dataiku_container_settings.test", "default_k8s_cluster_id", "some-cluster"),
			},
		},
		CheckDestroy: func(*terraform.State) error {
			value, present := fake.generalSetting("useImplicitK8sCluster")
			if !present {
				return fmt.Errorf("useImplicitK8sCluster was removed from the document; it existed before the apply")
			}
			if value != false {
				return fmt.Errorf("useImplicitK8sCluster is %v after destroy, want false: the value it held before Terraform took it over", value)
			}
			if _, present := fake.generalSetting("defaultK8sClusterId"); present {
				return fmt.Errorf("defaultK8sClusterId is still set after destroy; it was absent before the apply and should have been removed again")
			}
			return nil
		},
	})
}

// TestAccContainerSettingsReportsAFieldDSSDiscards is the guard that makes the
// rest of this resource trustworthy.
//
// DSS answers 200 for a general settings document carrying a field it does not
// recognise, stores everything else, and leaves that field out. Without the
// read-back the apply would succeed and the instance would be unchanged.
func TestAccContainerSettingsReportsAFieldDSSDiscards(t *testing.T) {
	fake := testAccSetup(t)
	if fake == nil {
		t.Skip("runs against the fake only: it needs a field the instance is known to discard")
	}
	fake.forgetSetting("useImplicitK8sCluster")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "dataiku_container_settings" "test" {
  use_implicit_k8s_cluster = true
}
`,
				ExpectError: regexp.MustCompile(`use_implicit_k8s_cluster \(dropped\)`),
			},
		},
	})
}
