package provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestAccContainerConfigs(t *testing.T) {
	testAccSetup(t)
	build := randName(t, "build")
	exec := randName(t, "exec")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "dataiku_container_image_build_config" "test" {
  name = %[1]q

  # No "type" here. An execution config has one; a build config does not, and
  # DSS drops it silently. Verified against DSS 15.
  settings_json = jsonencode({
    baseImageType    = "EXEC"
    imageBuilderType = "DOCKER"
  })
}

resource "dataiku_container_execution_config" "test" {
  name = %[2]q

  settings_json = jsonencode({
    type             = "KUBERNETES"
    imageBuildConfig = dataiku_container_image_build_config.test.name
    usableBy         = "ALL"
    allowedGroups    = []
  })
}
`, build, exec),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("dataiku_container_image_build_config.test", "id", build),
					resource.TestCheckResourceAttr("dataiku_container_image_build_config.test", "name", build),
					resource.TestCheckResourceAttr("dataiku_container_execution_config.test", "name", exec),
					// effective_json holds what DSS actually stored, which is
					// more than was sent.
					resource.TestMatchResourceAttr("dataiku_container_execution_config.test", "effective_json",
						regexp.MustCompile(`"imageBuildConfig":"`+regexp.QuoteMeta(build)+`"`)),
					resource.TestMatchResourceAttr("dataiku_container_execution_config.test", "effective_json",
						regexp.MustCompile(`"workloadType":"ANY"`)),
				),
			},
			{
				// An in-place update of the settings document.
				Config: fmt.Sprintf(`
resource "dataiku_container_image_build_config" "test" {
  name = %[1]q

  # No "type" here. An execution config has one; a build config does not, and
  # DSS drops it silently. Verified against DSS 15.
  settings_json = jsonencode({
    baseImageType    = "EXEC"
    imageBuilderType = "DOCKER"
  })
}

resource "dataiku_container_execution_config" "test" {
  name = %[2]q

  settings_json = jsonencode({
    type             = "KUBERNETES"
    imageBuildConfig = dataiku_container_image_build_config.test.name
    usableBy         = "ALLOWED"
    allowedGroups    = ["administrators"]
    workloadType     = "ANY"
  })
}
`, build, exec),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr("dataiku_container_execution_config.test", "effective_json",
						regexp.MustCompile(`"usableBy":"ALLOWED"`)),
				),
			},
			{
				ResourceName:      "dataiku_container_execution_config.test",
				ImportState:       true,
				ImportStateId:     exec,
				ImportStateVerify: false,
			},
		},
	})
}

// The behaviour this whole resource shape exists for: DSS answers 200 and
// stores nothing for a field it does not recognise at the level it was given.
// createNamespace belongs inside kubernetesRuntimeConfig; at the top level a
// real instance drops it and still reports success, so a typed resource would
// tell the practitioner a setting was applied when it was not.
func TestAccContainerConfigDroppedFieldFailsLoudly(t *testing.T) {
	fake := testAccSetup(t)
	if fake == nil {
		t.Skip("runs against the fake only: it asserts on a field this DSS version is known to drop")
	}
	build := randName(t, "build")
	exec := randName(t, "exec")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "dataiku_container_image_build_config" "test" {
  name          = %[1]q
  settings_json = jsonencode({ baseImageType = "EXEC" })
}

resource "dataiku_container_execution_config" "test" {
  name = %[2]q

  settings_json = jsonencode({
    type             = "KUBERNETES"
    imageBuildConfig = dataiku_container_image_build_config.test.name
    createNamespace  = true
  })
}
`, build, exec),
				ExpectError: regexp.MustCompile(`(?s)did not apply every setting.*createNamespace.*dropped`),
			},
		},
	})
}

// DSS validates this one reference rather than ignoring it, so a bad name
// fails the whole settings write.
func TestAccContainerExecutionConfigRejectsUnknownBuildConfig(t *testing.T) {
	testAccSetup(t)
	exec := randName(t, "exec")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "dataiku_container_execution_config" "test" {
  name = %[1]q

  settings_json = jsonencode({
    type             = "KUBERNETES"
    imageBuildConfig = "no-such-build-config"
  })
}
`, exec),
				ExpectError: regexp.MustCompile(`unknown image build configuration`),
			},
		},
	})
}

// name is managed by its own argument, so a name inside the JSON would be two
// sources of truth for one value.
func TestAccContainerConfigRejectsNameInSettings(t *testing.T) {
	testAccSetup(t)
	build := randName(t, "build")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "dataiku_container_image_build_config" "test" {
  name          = %[1]q
  settings_json = jsonencode({ name = "something-else", baseImageType = "EXEC" })
}
`, build),
				ExpectError: regexp.MustCompile(`name must not appear in settings_json`),
			},
		},
	})
}

// The configs live inside one large document the provider does not otherwise
// model, so every write must preserve the rest of it.
func TestAccContainerConfigPreservesTheRestOfGeneralSettings(t *testing.T) {
	fake := testAccSetup(t)
	if fake == nil {
		t.Skip("inspects the fake's stored document directly")
	}
	build := randName(t, "build")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "dataiku_container_image_build_config" "test" {
  name          = %[1]q
  settings_json = jsonencode({ baseImageType = "EXEC" })
}
`, build),
			},
		},
		CheckDestroy: func(*terraform.State) error {
			fake.mu.Lock()
			defer fake.mu.Unlock()
			if _, ok := fake.generalSettings["ldapSettings"]; !ok {
				return fmt.Errorf("writing a container configuration dropped ldapSettings from general settings")
			}
			return nil
		},
	})
}
