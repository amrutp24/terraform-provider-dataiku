package provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// testAccProtoV6ProviderFactories serves the provider in-process to the
// terraform binary the test framework drives.
var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"dataiku": providerserver.NewProtocol6WithError(New("test")()),
}

// testAccSetup starts a fake DSS instance and points the provider at it
// through the environment, which is also how a real run is configured.
func testAccSetup(t *testing.T) *fakeDSS {
	t.Helper()
	fake, host := newFakeDSS(t)
	t.Setenv("DATAIKU_HOST", host)
	t.Setenv("DATAIKU_API_KEY", "acceptance-test-key")
	return fake
}

func TestAccProject(t *testing.T) {
	fake := testAccSetup(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "dataiku_project" "test" {
  project_key = "ACCTEST"
  name        = "Acceptance test"
  owner       = "admin"
  short_desc  = "first"
  tags        = ["a", "b"]
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("dataiku_project.test", "id", "ACCTEST"),
					resource.TestCheckResourceAttr("dataiku_project.test", "project_key", "ACCTEST"),
					resource.TestCheckResourceAttr("dataiku_project.test", "name", "Acceptance test"),
					resource.TestCheckResourceAttr("dataiku_project.test", "owner", "admin"),
					resource.TestCheckResourceAttr("dataiku_project.test", "short_desc", "first"),
					resource.TestCheckResourceAttr("dataiku_project.test", "tags.#", "2"),
					resource.TestCheckResourceAttr("dataiku_project.test", "tags.0", "a"),
					// Delete options fall back to their documented defaults.
					resource.TestCheckResourceAttr("dataiku_project.test", "clear_managed_datasets_on_delete", "false"),
					resource.TestCheckResourceAttr("dataiku_project.test", "clear_job_and_scenario_logs_on_delete", "true"),
				),
			},
			{
				// An in-place update of every mutable field.
				Config: `
resource "dataiku_project" "test" {
  project_key = "ACCTEST"
  name        = "Renamed"
  owner       = "admin"
  short_desc  = "second"
  description = "A longer description"
  tags        = ["c"]
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("dataiku_project.test", "name", "Renamed"),
					resource.TestCheckResourceAttr("dataiku_project.test", "short_desc", "second"),
					resource.TestCheckResourceAttr("dataiku_project.test", "description", "A longer description"),
					resource.TestCheckResourceAttr("dataiku_project.test", "tags.#", "1"),
					resource.TestCheckResourceAttr("dataiku_project.test", "tags.0", "c"),
				),
			},
			{
				ResourceName:      "dataiku_project.test",
				ImportState:       true,
				ImportStateId:     "ACCTEST",
				ImportStateVerify: true,
				// project_folder_id is create-only and never reported by DSS.
				ImportStateVerifyIgnore: []string{
					"project_folder_id",
					"clear_managed_datasets_on_delete",
					"clear_output_managed_folders_on_delete",
					"clear_job_and_scenario_logs_on_delete",
				},
			},
		},
	})

	if fake.droppedUnmodelledField() {
		t.Error("a project update wrote back a document missing a field the provider does not model")
	}
	if got := fake.projectCount(); got != 0 {
		t.Errorf("after destroy the instance still holds %d project(s)", got)
	}
}

func TestAccProjectRequiresReplaceOnKeyChange(t *testing.T) {
	testAccSetup(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "dataiku_project" "test" {
  project_key = "FIRST"
  name        = "First"
  owner       = "admin"
}
`,
				Check: resource.TestCheckResourceAttr("dataiku_project.test", "id", "FIRST"),
			},
			{
				Config: `
resource "dataiku_project" "test" {
  project_key = "SECOND"
  name        = "First"
  owner       = "admin"
}
`,
				Check: resource.TestCheckResourceAttr("dataiku_project.test", "id", "SECOND"),
			},
		},
	})
}

func TestAccProjectRejectsInvalidKey(t *testing.T) {
	testAccSetup(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "dataiku_project" "test" {
  project_key = "has spaces"
  name        = "Bad"
  owner       = "admin"
}
`,
				ExpectError: regexp.MustCompile("only letters, digits and underscores"),
			},
		},
	})
}

func TestAccGroup(t *testing.T) {
	fake := testAccSetup(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "dataiku_group" "test" {
  name        = "data_scientists"
  description = "before"

  permissions = {
    mayCreateProjects = true
  }
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("dataiku_group.test", "id", "data_scientists"),
					resource.TestCheckResourceAttr("dataiku_group.test", "description", "before"),
					resource.TestCheckResourceAttr("dataiku_group.test", "source_type", "LOCAL"),
					resource.TestCheckResourceAttr("dataiku_group.test", "admin", "false"),
					resource.TestCheckResourceAttr("dataiku_group.test", "permissions.mayCreateProjects", "true"),
				),
			},
			{
				Config: `
resource "dataiku_group" "test" {
  name        = "data_scientists"
  description = "after"
  admin       = true

  permissions = {
    mayCreateProjects = false
  }
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("dataiku_group.test", "description", "after"),
					resource.TestCheckResourceAttr("dataiku_group.test", "admin", "true"),
					resource.TestCheckResourceAttr("dataiku_group.test", "permissions.mayCreateProjects", "false"),
				),
			},
		},
	})

	// The whole reason the client does read-modify-write.
	if fake.droppedUnmodelledField() {
		t.Error("a group update revoked an ability the provider does not model")
	}
}

func TestAccUser(t *testing.T) {
	fake := testAccSetup(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "dataiku_user" "test" {
  login        = "jsmith"
  display_name = "J. Smith"
  email        = "jsmith@example.com"
  user_profile = "FULL_DESIGNER"
  password     = "initial-secret"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("dataiku_user.test", "id", "jsmith"),
					resource.TestCheckResourceAttr("dataiku_user.test", "display_name", "J. Smith"),
					resource.TestCheckResourceAttr("dataiku_user.test", "email", "jsmith@example.com"),
					resource.TestCheckResourceAttr("dataiku_user.test", "user_profile", "FULL_DESIGNER"),
					resource.TestCheckResourceAttr("dataiku_user.test", "source_type", "LOCAL"),
					resource.TestCheckResourceAttr("dataiku_user.test", "enabled", "true"),
					resource.TestCheckResourceAttr("dataiku_user.test", "groups.#", "0"),
				),
			},
			{
				Config: `
resource "dataiku_group" "team" {
  name = "team"
}

resource "dataiku_user" "test" {
  login        = "jsmith"
  display_name = "Jane Smith"
  email        = "jane@example.com"
  user_profile = "DATA_DESIGNER"
  password     = "rotated-secret"
  enabled      = false
  groups       = [dataiku_group.team.name]
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("dataiku_user.test", "display_name", "Jane Smith"),
					resource.TestCheckResourceAttr("dataiku_user.test", "user_profile", "DATA_DESIGNER"),
					resource.TestCheckResourceAttr("dataiku_user.test", "enabled", "false"),
					resource.TestCheckResourceAttr("dataiku_user.test", "groups.#", "1"),
					resource.TestCheckResourceAttr("dataiku_user.test", "groups.0", "team"),
				),
			},
			{
				ResourceName:      "dataiku_user.test",
				ImportState:       true,
				ImportStateId:     "jsmith",
				ImportStateVerify: true,
				// DSS never returns the password.
				ImportStateVerifyIgnore: []string{"password"},
			},
		},
	})

	if fake.droppedUnmodelledField() {
		t.Error("a user update dropped a field the provider does not model")
	}
}

func TestAccConnection(t *testing.T) {
	fake := testAccSetup(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "dataiku_connection" "test" {
  name        = "warehouse"
  type        = "PostgreSQL"
  description = "before"

  params_json = jsonencode({
    host     = "db.example.com"
    port     = "5432"
    password = "super-secret"
  })
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("dataiku_connection.test", "id", "warehouse"),
					resource.TestCheckResourceAttr("dataiku_connection.test", "type", "PostgreSQL"),
					resource.TestCheckResourceAttr("dataiku_connection.test", "usable_by", "ALL"),
					resource.TestCheckResourceAttr("dataiku_connection.test", "allowed_groups.#", "0"),
					func(_ *terraform.State) error {
						if got := fake.storedConnectionPassword("warehouse"); got != "super-secret" {
							return fmt.Errorf("stored password = %q, want super-secret", got)
						}
						return nil
					},
				),
			},
			{
				// Change only the description. The redacted password DSS
				// returns must not be written back over the real one.
				Config: `
resource "dataiku_group" "readers" {
  name = "readers"
}

resource "dataiku_connection" "test" {
  name        = "warehouse"
  type        = "PostgreSQL"
  description = "after"

  params_json = jsonencode({
    host     = "db.example.com"
    port     = "5432"
    password = "super-secret"
  })

  usable_by      = "ALLOWED"
  allowed_groups = [dataiku_group.readers.name]
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("dataiku_connection.test", "description", "after"),
					resource.TestCheckResourceAttr("dataiku_connection.test", "usable_by", "ALLOWED"),
					resource.TestCheckResourceAttr("dataiku_connection.test", "allowed_groups.0", "readers"),
					func(_ *terraform.State) error {
						if got := fake.storedConnectionPassword("warehouse"); got != "super-secret" {
							return fmt.Errorf("the update destroyed the stored password: got %q", got)
						}
						return nil
					},
				),
			},
		},
	})
}

func TestAccProjectPermissions(t *testing.T) {
	testAccSetup(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "dataiku_project" "test" {
  project_key = "PERMS"
  name        = "Perms"
  owner       = "admin"
}

resource "dataiku_group" "ds" {
  name = "ds"
}

resource "dataiku_project_permissions" "test" {
  project_key = dataiku_project.test.project_key

  permission {
    group                 = dataiku_group.ds.name
    read_project_content  = true
    write_project_content = true
    run_scenario          = true
  }
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("dataiku_project_permissions.test", "id", "PERMS"),
					resource.TestCheckResourceAttr("dataiku_project_permissions.test", "owner", "admin"),
					resource.TestCheckResourceAttr("dataiku_project_permissions.test", "permission.#", "1"),
					resource.TestCheckResourceAttr("dataiku_project_permissions.test", "permission.0.group", "ds"),
					resource.TestCheckResourceAttr("dataiku_project_permissions.test", "permission.0.read_project_content", "true"),
					resource.TestCheckResourceAttr("dataiku_project_permissions.test", "permission.0.run_scenario", "true"),
					resource.TestCheckResourceAttr("dataiku_project_permissions.test", "permission.0.admin", "false"),
				),
			},
			{
				Config: `
resource "dataiku_project" "test" {
  project_key = "PERMS"
  name        = "Perms"
  owner       = "admin"
}

resource "dataiku_group" "ds" {
  name = "ds"
}

resource "dataiku_group" "readers" {
  name = "readers"
}

resource "dataiku_project_permissions" "test" {
  project_key = dataiku_project.test.project_key

  permission {
    group                = dataiku_group.ds.name
    read_project_content = true
  }

  permission {
    group           = dataiku_group.readers.name
    read_dashboards = true
  }
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("dataiku_project_permissions.test", "permission.#", "2"),
					resource.TestCheckResourceAttr("dataiku_project_permissions.test", "permission.0.write_project_content", "false"),
					resource.TestCheckResourceAttr("dataiku_project_permissions.test", "permission.1.group", "readers"),
					resource.TestCheckResourceAttr("dataiku_project_permissions.test", "permission.1.read_dashboards", "true"),
				),
			},
		},
	})
}

func TestAccProjectVariables(t *testing.T) {
	testAccSetup(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "dataiku_project" "test" {
  project_key = "VARS"
  name        = "Vars"
  owner       = "admin"
}

resource "dataiku_project_variables" "test" {
  project_key = dataiku_project.test.project_key
  standard    = jsonencode({ currency = "USD", lookback = 90 })
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("dataiku_project_variables.test", "id", "VARS"),
					resource.TestCheckResourceAttr("dataiku_project_variables.test", "standard", `{"currency":"USD","lookback":90}`),
					resource.TestCheckResourceAttr("dataiku_project_variables.test", "local", `{}`),
				),
			},
			{
				Config: `
resource "dataiku_project" "test" {
  project_key = "VARS"
  name        = "Vars"
  owner       = "admin"
}

resource "dataiku_project_variables" "test" {
  project_key = dataiku_project.test.project_key
  standard    = jsonencode({ currency = "EUR" })
  local       = jsonencode({ environment = "staging" })
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("dataiku_project_variables.test", "standard", `{"currency":"EUR"}`),
					resource.TestCheckResourceAttr("dataiku_project_variables.test", "local", `{"environment":"staging"}`),
				),
			},
			{
				// Whitespace-only changes must not produce a diff.
				Config: `
resource "dataiku_project" "test" {
  project_key = "VARS"
  name        = "Vars"
  owner       = "admin"
}

resource "dataiku_project_variables" "test" {
  project_key = dataiku_project.test.project_key
  standard    = "{ \"currency\" : \"EUR\" }"
  local       = jsonencode({ environment = "staging" })
}
`,
				PlanOnly: true,
			},
		},
	})
}

func TestAccDataSources(t *testing.T) {
	testAccSetup(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "dataiku_project" "test" {
  project_key = "DSTEST"
  name        = "Data source test"
  owner       = "admin"
  tags        = ["x"]
}

resource "dataiku_group" "test" {
  name        = "dsgroup"
  description = "read me"
}

resource "dataiku_user" "test" {
  login        = "dsuser"
  display_name = "DS User"
  password     = "secret"
}

data "dataiku_project" "test" {
  project_key = dataiku_project.test.project_key
}

data "dataiku_projects" "all" {
  depends_on = [dataiku_project.test]
}

data "dataiku_group" "test" {
  name = dataiku_group.test.name
}

data "dataiku_user" "test" {
  login = dataiku_user.test.login
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.dataiku_project.test", "name", "Data source test"),
					resource.TestCheckResourceAttr("data.dataiku_project.test", "owner", "admin"),
					resource.TestCheckResourceAttr("data.dataiku_project.test", "tags.0", "x"),
					resource.TestCheckResourceAttr("data.dataiku_projects.all", "project_keys.#", "1"),
					resource.TestCheckResourceAttr("data.dataiku_projects.all", "projects.0.project_key", "DSTEST"),
					resource.TestCheckResourceAttr("data.dataiku_group.test", "description", "read me"),
					// definition_json is how users discover ability names.
					resource.TestCheckResourceAttrSet("data.dataiku_group.test", "definition_json"),
					resource.TestCheckResourceAttr("data.dataiku_user.test", "display_name", "DS User"),
					resource.TestCheckResourceAttr("data.dataiku_user.test", "enabled", "true"),
				),
			},
		},
	})
}
