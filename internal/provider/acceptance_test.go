package provider

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/amrutp24/terraform-provider-dataiku/internal/dataiku"
)

// testAccProtoV6ProviderFactories serves the provider in-process to the
// terraform binary the test framework drives.
var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"dataiku": providerserver.NewProtocol6WithError(New("test")()),
}

// testAccSetup decides what the tests run against. With DATAIKU_HOST set they
// use that instance, which is how you validate the provider against real DSS.
// Otherwise they start an in-process fake, so the suite is runnable with no
// instance at all.
//
// It returns nil when running against a real instance, because the assertions
// that inspect what the server stored can only be made against the fake.
func testAccSetup(t *testing.T) *fakeDSS {
	t.Helper()

	if host := os.Getenv("DATAIKU_HOST"); host != "" {
		if os.Getenv("DATAIKU_API_KEY") == "" {
			t.Fatal("DATAIKU_HOST is set but DATAIKU_API_KEY is not; both are needed to test against a real instance")
		}
		t.Logf("running against the DSS instance at %s", host)
		return nil
	}

	fake, host := newFakeDSS(t)
	t.Setenv("DATAIKU_HOST", host)
	t.Setenv("DATAIKU_API_KEY", "acceptance-test-key")
	return fake
}

// Names are randomised per run so the tests never collide with objects DSS
// ships by default (the "readers" and "data_team" groups, for one) or with
// leftovers from an earlier run that failed before its cleanup.
func randProjectKey(t *testing.T) string {
	t.Helper()
	return "TFACC" + strings.ToUpper(acctest.RandStringFromCharSet(8, acctest.CharSetAlpha))
}

func randName(t *testing.T, kind string) string {
	t.Helper()
	return "tfacc_" + kind + "_" + strings.ToLower(acctest.RandStringFromCharSet(8, acctest.CharSetAlpha))
}

// Which licence profiles a DSS instance grants depends on its licence, so the
// two the user test switches between are configurable. The defaults match what
// a Dataiku trial licences; read /public/api/admin/licensing/status to find
// what yours does.
func testAccUserProfiles(t *testing.T) (string, string) {
	t.Helper()
	first := os.Getenv("DATAIKU_TEST_USER_PROFILE")
	if first == "" {
		first = "DESIGNER"
	}
	second := os.Getenv("DATAIKU_TEST_USER_PROFILE_ALT")
	if second == "" {
		second = "NONE"
	}
	return first, second
}

func TestAccProject(t *testing.T) {
	fake := testAccSetup(t)
	key := randProjectKey(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "dataiku_project" "test" {
  project_key = %[1]q
  name        = "Acceptance test"
  owner       = "admin"
  short_desc  = "first"
  tags        = ["a", "b"]
}
`, key),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("dataiku_project.test", "id", key),
					resource.TestCheckResourceAttr("dataiku_project.test", "project_key", key),
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
				Config: fmt.Sprintf(`
resource "dataiku_project" "test" {
  project_key = %[1]q
  name        = "Renamed"
  owner       = "admin"
  short_desc  = "second"
  description = "A longer description"
  tags        = ["c"]
}
`, key),
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
				ImportStateId:     key,
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

	if fake == nil {
		return
	}
	if fake.droppedUnmodelledField() {
		t.Error("a project update wrote back a document missing a field the provider does not model")
	}
	if got := fake.projectCount(); got != 0 {
		t.Errorf("after destroy the instance still holds %d project(s)", got)
	}
}

func TestAccProjectRequiresReplaceOnKeyChange(t *testing.T) {
	testAccSetup(t)
	first := randProjectKey(t)
	second := randProjectKey(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "dataiku_project" "test" {
  project_key = %[1]q
  name        = "First"
  owner       = "admin"
}
`, first),
				Check: resource.TestCheckResourceAttr("dataiku_project.test", "id", first),
			},
			{
				Config: fmt.Sprintf(`
resource "dataiku_project" "test" {
  project_key = %[1]q
  name        = "First"
  owner       = "admin"
}
`, second),
				Check: resource.TestCheckResourceAttr("dataiku_project.test", "id", second),
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
	name := randName(t, "grp")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "dataiku_group" "test" {
  name        = %[1]q
  description = "before"

  permissions = {
    mayCreateProjects = true
  }
}
`, name),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("dataiku_group.test", "id", name),
					resource.TestCheckResourceAttr("dataiku_group.test", "description", "before"),
					resource.TestCheckResourceAttr("dataiku_group.test", "source_type", "LOCAL"),
					resource.TestCheckResourceAttr("dataiku_group.test", "admin", "false"),
					resource.TestCheckResourceAttr("dataiku_group.test", "permissions.mayCreateProjects", "true"),
					// DSS returns this as an array, not a string.
					resource.TestCheckResourceAttr("dataiku_group.test", "ldap_group_names.#", "0"),
				),
			},
			{
				Config: fmt.Sprintf(`
resource "dataiku_group" "test" {
  name        = %[1]q
  description = "after"
  admin       = true

  permissions = {
    mayCreateProjects = false
  }
}
`, name),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("dataiku_group.test", "description", "after"),
					resource.TestCheckResourceAttr("dataiku_group.test", "admin", "true"),
					resource.TestCheckResourceAttr("dataiku_group.test", "permissions.mayCreateProjects", "false"),
				),
			},
		},
	})

	// The whole reason the client does read-modify-write.
	if fake != nil && fake.droppedUnmodelledField() {
		t.Error("a group update revoked an ability the provider does not model")
	}
}

func TestAccUser(t *testing.T) {
	fake := testAccSetup(t)
	login := randName(t, "usr")
	group := randName(t, "grp")
	profile, altProfile := testAccUserProfiles(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "dataiku_user" "test" {
  login        = %[1]q
  display_name = "J. Smith"
  email        = "%[1]s@example.com"
  user_profile = %[2]q
  password     = "initial-secret"
}
`, login, profile),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("dataiku_user.test", "id", login),
					resource.TestCheckResourceAttr("dataiku_user.test", "display_name", "J. Smith"),
					resource.TestCheckResourceAttr("dataiku_user.test", "user_profile", profile),
					resource.TestCheckResourceAttr("dataiku_user.test", "source_type", "LOCAL"),
					resource.TestCheckResourceAttr("dataiku_user.test", "enabled", "true"),
					// Not asserted as empty: DSS puts a new user in a default
					// group when the configuration names none.
					resource.TestCheckResourceAttrSet("dataiku_user.test", "groups.#"),
				),
			},
			{
				Config: fmt.Sprintf(`
resource "dataiku_group" "team" {
  name = %[2]q
}

resource "dataiku_user" "test" {
  login        = %[1]q
  display_name = "Jane Smith"
  email        = "jane@example.com"
  user_profile = %[3]q
  password     = "rotated-secret"
  enabled      = false
  groups       = [dataiku_group.team.name]
}
`, login, group, altProfile),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("dataiku_user.test", "display_name", "Jane Smith"),
					resource.TestCheckResourceAttr("dataiku_user.test", "user_profile", altProfile),
					resource.TestCheckResourceAttr("dataiku_user.test", "enabled", "false"),
					resource.TestCheckResourceAttr("dataiku_user.test", "groups.#", "1"),
					resource.TestCheckResourceAttr("dataiku_user.test", "groups.0", group),
				),
			},
			{
				ResourceName:      "dataiku_user.test",
				ImportState:       true,
				ImportStateId:     login,
				ImportStateVerify: true,
				// DSS never returns the password, and an import deliberately
				// adopts the user's full group membership, including the
				// per-user group DSS attaches itself, where the managed state
				// tracks only the memberships the configuration names.
				ImportStateVerifyIgnore: []string{"password", "groups.#", "groups.1"},
			},
		},
	})

	if fake != nil && fake.droppedUnmodelledField() {
		t.Error("a user update dropped a field the provider does not model")
	}
}

func TestAccConnection(t *testing.T) {
	fake := testAccSetup(t)
	name := randName(t, "conn")
	group := randName(t, "grp")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "dataiku_connection" "test" {
  name        = %[1]q
  type        = "PostgreSQL"
  description = "before"

  params_json = jsonencode({
    host     = "db.example.com"
    port     = "5432"
    password = "super-secret"
  })
}
`, name),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("dataiku_connection.test", "id", name),
					resource.TestCheckResourceAttr("dataiku_connection.test", "type", "PostgreSQL"),
					resource.TestCheckResourceAttr("dataiku_connection.test", "usable_by", "ALL"),
					resource.TestCheckResourceAttr("dataiku_connection.test", "allowed_groups.#", "0"),
					func(_ *terraform.State) error {
						if fake == nil {
							return nil
						}
						if got := fake.storedConnectionPassword(name); got != "super-secret" {
							return fmt.Errorf("stored password = %q, want super-secret", got)
						}
						return nil
					},
				),
			},
			{
				// Change only the description and access control. The redacted
				// password DSS returns must not be written back over the real one.
				Config: fmt.Sprintf(`
resource "dataiku_group" "readers" {
  name = %[2]q
}

resource "dataiku_connection" "test" {
  name        = %[1]q
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
`, name, group),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("dataiku_connection.test", "description", "after"),
					resource.TestCheckResourceAttr("dataiku_connection.test", "usable_by", "ALLOWED"),
					resource.TestCheckResourceAttr("dataiku_connection.test", "allowed_groups.0", group),
					func(_ *terraform.State) error {
						if fake == nil {
							return nil
						}
						if got := fake.storedConnectionPassword(name); got != "super-secret" {
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
	key := randProjectKey(t)
	groupA := randName(t, "grp")
	groupB := randName(t, "grp")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "dataiku_project" "test" {
  project_key = %[1]q
  name        = "Perms"
  owner       = "admin"
}

resource "dataiku_group" "a" {
  name = %[2]q
}

resource "dataiku_project_permissions" "test" {
  project_key = dataiku_project.test.project_key

  permission {
    group                 = dataiku_group.a.name
    read_project_content  = true
    write_project_content = true
    run_scenarios         = true
  }
}
`, key, groupA),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("dataiku_project_permissions.test", "id", key),
					resource.TestCheckResourceAttr("dataiku_project_permissions.test", "owner", "admin"),
					resource.TestCheckResourceAttr("dataiku_project_permissions.test", "permission.#", "1"),
					resource.TestCheckResourceAttr("dataiku_project_permissions.test", "permission.0.group", groupA),
					resource.TestCheckResourceAttr("dataiku_project_permissions.test", "permission.0.read_project_content", "true"),
					resource.TestCheckResourceAttr("dataiku_project_permissions.test", "permission.0.run_scenarios", "true"),
					resource.TestCheckResourceAttr("dataiku_project_permissions.test", "permission.0.admin", "false"),
				),
			},
			{
				Config: fmt.Sprintf(`
resource "dataiku_project" "test" {
  project_key = %[1]q
  name        = "Perms"
  owner       = "admin"
}

resource "dataiku_group" "a" {
  name = %[2]q
}

resource "dataiku_group" "b" {
  name = %[3]q
}

resource "dataiku_project_permissions" "test" {
  project_key = dataiku_project.test.project_key

  permission {
    group                = dataiku_group.a.name
    read_project_content = true
  }

  permission {
    group           = dataiku_group.b.name
    read_dashboards = true
  }
}
`, key, groupA, groupB),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("dataiku_project_permissions.test", "permission.#", "2"),
					resource.TestCheckResourceAttr("dataiku_project_permissions.test", "permission.0.write_project_content", "false"),
					resource.TestCheckResourceAttr("dataiku_project_permissions.test", "permission.1.group", groupB),
					resource.TestCheckResourceAttr("dataiku_project_permissions.test", "permission.1.read_dashboards", "true"),
				),
			},
		},
	})
}

func TestAccProjectVariables(t *testing.T) {
	testAccSetup(t)
	key := randProjectKey(t)

	project := fmt.Sprintf(`
resource "dataiku_project" "test" {
  project_key = %[1]q
  name        = "Vars"
  owner       = "admin"
}
`, key)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: project + `
resource "dataiku_project_variables" "test" {
  project_key = dataiku_project.test.project_key
  standard    = jsonencode({ currency = "USD", lookback = 90 })
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("dataiku_project_variables.test", "id", key),
					resource.TestCheckResourceAttr("dataiku_project_variables.test", "standard", `{"currency":"USD","lookback":90}`),
					resource.TestCheckResourceAttr("dataiku_project_variables.test", "local", `{}`),
				),
			},
			{
				Config: project + `
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
				// Reformatting the JSON must not plan as a change.
				Config: project + `
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
	key := randProjectKey(t)
	group := randName(t, "grp")
	login := randName(t, "usr")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "dataiku_project" "test" {
  project_key = %[1]q
  name        = "Data source test"
  owner       = "admin"
  tags        = ["x"]
}

resource "dataiku_group" "test" {
  name        = %[2]q
  description = "read me"
}

resource "dataiku_user" "test" {
  login        = %[3]q
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
`, key, group, login),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.dataiku_project.test", "name", "Data source test"),
					resource.TestCheckResourceAttr("data.dataiku_project.test", "owner", "admin"),
					resource.TestCheckResourceAttr("data.dataiku_project.test", "tags.0", "x"),
					// Assert the project is listed rather than that it is the
					// only one, so this also holds on an instance that already
					// has projects of its own.
					checkListContains("data.dataiku_projects.all", "project_keys", key),
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

// checkListContains asserts that a list attribute holds want somewhere in it,
// without pinning the list's length or the element's position.
func checkListContains(resourceName, attribute, want string) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		rs, ok := state.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("%s is not in state", resourceName)
		}

		prefix := attribute + "."
		found := []string{}
		for key, value := range rs.Primary.Attributes {
			if !strings.HasPrefix(key, prefix) || strings.HasSuffix(key, ".#") {
				continue
			}
			if value == want {
				return nil
			}
			found = append(found, value)
		}

		sort.Strings(found)
		return fmt.Errorf("%s.%s does not contain %q; it holds %v", resourceName, attribute, want, found)
	}
}

func TestAccCodeEnv(t *testing.T) {
	testAccSetup(t)
	name := randName(t, "env")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				// install_packages_on_change is off throughout: resolving
				// packages runs pip on the instance and would make this test
				// depend on a package index being reachable.
				Config: fmt.Sprintf(`
resource "dataiku_code_env" "test" {
  name = %[1]q
  lang = "PYTHON"

  packages                   = "scikit-learn==1.5.0"
  install_packages_on_change = false
}
`, name),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("dataiku_code_env.test", "id", "PYTHON/"+name),
					resource.TestCheckResourceAttr("dataiku_code_env.test", "name", name),
					resource.TestCheckResourceAttr("dataiku_code_env.test", "lang", "PYTHON"),
					resource.TestCheckResourceAttr("dataiku_code_env.test", "deployment_mode", "DESIGN_MANAGED"),
					resource.TestCheckResourceAttr("dataiku_code_env.test", "packages", "scikit-learn==1.5.0"),
					resource.TestCheckResourceAttr("dataiku_code_env.test", "conda", "false"),
					resource.TestCheckResourceAttrSet("dataiku_code_env.test", "python_interpreter"),
				),
			},
			{
				Config: fmt.Sprintf(`
resource "dataiku_code_env" "test" {
  name = %[1]q
  lang = "PYTHON"

  packages                   = "scikit-learn==1.5.0\npandas==2.2.2"
  install_jupyter_support    = true
  usable_by_all              = false
  install_packages_on_change = false
}
`, name),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("dataiku_code_env.test", "packages", "scikit-learn==1.5.0\npandas==2.2.2"),
					resource.TestCheckResourceAttr("dataiku_code_env.test", "install_jupyter_support", "true"),
					resource.TestCheckResourceAttr("dataiku_code_env.test", "usable_by_all", "false"),
				),
			},
			{
				ResourceName:      "dataiku_code_env.test",
				ImportState:       true,
				ImportStateId:     "PYTHON/" + name,
				ImportStateVerify: true,
				// Terraform-local only; the instance stores nothing for it.
				ImportStateVerifyIgnore: []string{"install_packages_on_change"},
			},
		},
	})
}

func TestAccProjectFolder(t *testing.T) {
	if testAccSetup(t) != nil {
		t.Skip("project folders are only exercised against a real instance")
	}
	name := randName(t, "fld")
	renamed := randName(t, "fld")
	child := randName(t, "fld")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "dataiku_project_folder" "test" {
  name = %[1]q
}
`, name),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("dataiku_project_folder.test", "name", name),
					resource.TestCheckResourceAttr("dataiku_project_folder.test", "parent_id", "ROOT"),
					resource.TestCheckResourceAttrSet("dataiku_project_folder.test", "id"),
					resource.TestCheckResourceAttrSet("dataiku_project_folder.test", "owner"),
				),
			},
			{
				// A rename is an in-place settings change, and nesting is a
				// move: both must keep the same folder id.
				Config: fmt.Sprintf(`
resource "dataiku_project_folder" "parent" {
  name = %[3]q
}

resource "dataiku_project_folder" "test" {
  name      = %[2]q
  parent_id = dataiku_project_folder.parent.id
}
`, name, renamed, child),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("dataiku_project_folder.test", "name", renamed),
					resource.TestCheckResourceAttrPair(
						"dataiku_project_folder.test", "parent_id",
						"dataiku_project_folder.parent", "id",
					),
				),
			},
			{
				ResourceName:      "dataiku_project_folder.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccProjectInFolder(t *testing.T) {
	if testAccSetup(t) != nil {
		t.Skip("project folders are only exercised against a real instance")
	}
	folder := randName(t, "fld")
	key := randProjectKey(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				// The pairing dataiku_project_folder exists for: placing a
				// project somewhere other than the root.
				Config: fmt.Sprintf(`
resource "dataiku_project_folder" "test" {
  name = %[1]q
}

resource "dataiku_project" "test" {
  project_key       = %[2]q
  name              = "In a folder"
  owner             = "admin"
  project_folder_id = dataiku_project_folder.test.id
}
`, folder, key),
				Check: resource.TestCheckResourceAttr("dataiku_project.test", "project_key", key),
			},
			{
				// A second step, because the data source can only observe the
				// project once it exists: the folder's contents are a reverse
				// reference, so nothing can see them in the apply that creates
				// the project.
				Config: fmt.Sprintf(`
resource "dataiku_project_folder" "test" {
  name = %[1]q
}

resource "dataiku_project" "test" {
  project_key       = %[2]q
  name              = "In a folder"
  owner             = "admin"
  project_folder_id = dataiku_project_folder.test.id
}

data "dataiku_project_folder" "test" {
  id = dataiku_project_folder.test.id
}
`, folder, key),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.dataiku_project_folder.test", "project_keys.#", "1"),
					resource.TestCheckResourceAttr("data.dataiku_project_folder.test", "project_keys.0", key),
					resource.TestCheckResourceAttr("data.dataiku_project_folder.test", "name", folder),
				),
			},
		},
	})
}

func TestAccScenario(t *testing.T) {
	testAccSetup(t)
	key := randProjectKey(t)

	project := fmt.Sprintf(`
resource "dataiku_project" "test" {
  project_key = %[1]q
  name        = "Scenario host"
  owner       = "admin"
}
`, key)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: project + `
resource "dataiku_scenario" "test" {
  project_key = dataiku_project.test.project_key
  name        = "Nightly build"

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
    name = "Build"
    type = "build_flowitem"
    params = {
      builds = []
    }
  }])
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					// The id is derived from the name, not random.
					resource.TestCheckResourceAttr("dataiku_scenario.test", "scenario_id", "Nightly_build"),
					resource.TestCheckResourceAttr("dataiku_scenario.test", "id", key+"/Nightly_build"),
					resource.TestCheckResourceAttr("dataiku_scenario.test", "name", "Nightly build"),
					resource.TestCheckResourceAttr("dataiku_scenario.test", "type", "step_based"),
					resource.TestCheckResourceAttr("dataiku_scenario.test", "active", "false"),
				),
			},
			{
				// Renaming and arming it must not replace the scenario, so the
				// id has to survive.
				Config: project + `
resource "dataiku_scenario" "test" {
  project_key = dataiku_project.test.project_key
  name        = "Nightly build (renamed)"
  active      = true
  tags        = ["managed-by-terraform"]

  triggers_json = jsonencode([{
    id     = "nightly"
    name   = "nightly"
    type   = "temporal"
    active = true
    params = {
      repeatFrequency = 1
      hour            = 4
      minute          = 30
      timezone        = "SERVER"
    }
  }])

  steps_json = jsonencode([{
    id   = "build"
    name = "Build"
    type = "build_flowitem"
    params = {
      builds = []
    }
  }])
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("dataiku_scenario.test", "scenario_id", "Nightly_build"),
					resource.TestCheckResourceAttr("dataiku_scenario.test", "name", "Nightly build (renamed)"),
					resource.TestCheckResourceAttr("dataiku_scenario.test", "active", "true"),
					resource.TestCheckResourceAttr("dataiku_scenario.test", "tags.0", "managed-by-terraform"),
				),
			},
			{
				// The JSON attributes are written and not read back, so a
				// second plan on the same configuration must be empty rather
				// than diffing against what DSS rewrote them to.
				Config:   project + testAccScenarioRenamedConfig,
				PlanOnly: true,
			},
		},
	})
}

// Kept out of the step above so the PlanOnly step is byte-identical to the
// configuration that was applied.
const testAccScenarioRenamedConfig = `
resource "dataiku_scenario" "test" {
  project_key = dataiku_project.test.project_key
  name        = "Nightly build (renamed)"
  active      = true
  tags        = ["managed-by-terraform"]

  triggers_json = jsonencode([{
    id     = "nightly"
    name   = "nightly"
    type   = "temporal"
    active = true
    params = {
      repeatFrequency = 1
      hour            = 4
      minute          = 30
      timezone        = "SERVER"
    }
  }])

  steps_json = jsonencode([{
    id   = "build"
    name = "Build"
    type = "build_flowitem"
    params = {
      builds = []
    }
  }])
}
`

func TestAccScenarioCustomPython(t *testing.T) {
	if testAccSetup(t) != nil {
		t.Skip("custom_python scenarios are only exercised against a real instance")
	}
	key := randProjectKey(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "dataiku_project" "test" {
  project_key = %[1]q
  name        = "Scenario host"
  owner       = "admin"
}

resource "dataiku_scenario" "test" {
  project_key = dataiku_project.test.project_key
  name        = "Scripted"
  type        = "custom_python"

  script = "scenario.set_scenario_output('ok')\n"
}
`, key),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("dataiku_scenario.test", "type", "custom_python"),
					// Unlike the JSON attributes, the script is stored verbatim
					// and does come back.
					resource.TestCheckResourceAttr("dataiku_scenario.test", "script", "scenario.set_scenario_output('ok')\n"),
				),
			},
		},
	})
}

// TestAccConnectionReachesDatabase is the test the connection resource was
// missing: every other one checks that DSS accepted the document, not that the
// connection it describes actually works. This one asks DSS to dial the
// database.
//
// It needs a database DSS itself can reach, which the in-process fake cannot
// provide, so it is skipped unless one is named. dev/README.md explains how to
// run one alongside the DSS container.
func TestAccConnectionReachesDatabase(t *testing.T) {
	if testAccSetup(t) != nil {
		t.Skip("needs a real DSS instance")
	}

	host := os.Getenv("DATAIKU_TEST_PG_HOST")
	if host == "" {
		t.Skip("set DATAIKU_TEST_PG_HOST (and _PORT/_DB/_USER/_PASSWORD) to test against a real database")
	}
	port := envOrDefault("DATAIKU_TEST_PG_PORT", "5432")
	db := envOrDefault("DATAIKU_TEST_PG_DB", "probe")
	user := envOrDefault("DATAIKU_TEST_PG_USER", "dku")
	password := envOrDefault("DATAIKU_TEST_PG_PASSWORD", "probe-pw")

	name := randName(t, "conn")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "dataiku_connection" "test" {
  name = %[1]q
  type = "PostgreSQL"

  params_json = jsonencode({
    host     = %[2]q
    port     = %[3]q
    db       = %[4]q
    user     = %[5]q
    password = %[6]q
  })
}
`, name, host, port, db, user, password),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("dataiku_connection.test", "type", "PostgreSQL"),
					checkConnectionReachesDatabase(name),
				),
			},
		},
	})
}

// checkConnectionReachesDatabase asks the instance whether the connection
// Terraform just created can actually be dialled.
func checkConnectionReachesDatabase(name string) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		client, err := dataiku.NewClient(dataiku.Config{
			Host:   os.Getenv("DATAIKU_HOST"),
			APIKey: os.Getenv("DATAIKU_API_KEY"),
		})
		if err != nil {
			return fmt.Errorf("building a client to test the connection: %w", err)
		}

		ok, err := client.TestConnection(context.Background(), name)
		if err != nil {
			return fmt.Errorf("asking DSS to test connection %q: %w", name, err)
		}
		if !ok {
			return fmt.Errorf("DSS reported connection %q as not working; the resource was accepted but does not connect", name)
		}
		return nil
	}
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
