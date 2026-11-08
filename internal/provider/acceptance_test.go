package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"

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
		t.Cleanup(func() { testAccSweepPerUserGroups(t) })
		return nil
	}

	fake, host := newFakeDSS(t)
	t.Setenv("DATAIKU_HOST", host)
	t.Setenv("DATAIKU_API_KEY", "acceptance-test-key")
	// Registered after the fake, so it runs before the server is closed:
	// cleanups run in reverse order of registration.
	t.Cleanup(func() { testAccSweepPerUserGroups(t) })
	return fake
}

// testAccInstanceGroups are the groups a stock DSS instance ships with. The
// sweep below picks its targets by the shape of a group's name, and this is
// the hard stop that keeps it off the instance's own groups even if one of
// them takes that shape: none of these three does, but a group named
// something like "data-team-eu-west1" would. Add your instance's own groups
// here before pointing the suite at an instance that has any.
var testAccInstanceGroups = map[string]bool{
	"administrators": true,
	"data_team":      true,
	"readers":        true,
}

// testAccPerUserGroupName matches the name DSS gives the group it creates for
// a user without being asked: <login>-<creator>-<random>. The last two
// segments are matched narrowly and the login greedily, so a login with a dash
// in it still ends up whole in the first submatch.
var testAccPerUserGroupName = regexp.MustCompile(`^(.+)-[^-]+-[A-Za-z0-9]{4,}$`)

// testAccSweepPerUserGroups deletes the groups the suite caused DSS to create
// but was never told about.
//
// Creating a user through the public API also creates a second group, named
// <login>-<creator>-<random> and carrying "admin": true even for a user whose
// profile is NONE and whose groups are empty. DSS does not delete it when the
// user goes, and Terraform cannot delete it because it never appeared in any
// configuration, so each run of this suite used to leave one behind for good.
// One test instance had collected thirteen, eleven of them belonging to users
// that no longer existed.
//
// The provider deliberately does not do this. A provider that deletes objects
// the configuration never declared is a provider you cannot trust with a
// shared instance, so the tidying belongs to whatever made the mess: this
// suite.
//
// A failure here is logged and swallowed. A group left behind is untidy; a
// test that fails because cleanup could not reach the instance is worse.
func testAccSweepPerUserGroups(t *testing.T) {
	t.Helper()

	client, err := dataiku.NewClient(dataiku.Config{
		Host:   os.Getenv("DATAIKU_HOST"),
		APIKey: os.Getenv("DATAIKU_API_KEY"),
	})
	if err != nil {
		t.Logf("not sweeping leftover per-user groups: building a client failed: %s", err)
		return
	}

	ctx := context.Background()
	users, err := client.ListUsers(ctx)
	if err != nil {
		t.Logf("not sweeping leftover per-user groups: listing users failed: %s", err)
		return
	}
	groups, err := client.ListGroups(ctx)
	if err != nil {
		t.Logf("not sweeping leftover per-user groups: listing groups failed: %s", err)
		return
	}

	for _, name := range testAccOrphanedPerUserGroups(groups, users) {
		if err := client.DeleteGroup(ctx, name); err != nil {
			t.Logf("deleting the leftover per-user group %q failed: %s", name, err)
			continue
		}
		t.Logf("swept the leftover per-user group %q", name)
	}
}

// testAccOrphanedPerUserGroups picks the groups that are safe to delete, which
// is a much smaller set than the groups that look like leftovers. All three
// conditions matter.
//
// The name has to have the shape DSS generates. Anything else is somebody's
// own group.
//
// The login in it must no longer exist. A group whose user is still there is a
// live membership, whatever the suite thinks of how it came about.
//
// Nobody may be in it. A hand-made group can happen to have three
// dash-separated segments, and deleting one because its first segment is not a
// login would be the worst thing this helper could do. Membership is the
// evidence that a group is in use, and DSS reports it on the user rather than
// on the group, which the single ListUsers call above already has.
func testAccOrphanedPerUserGroups(groups []dataiku.GroupListItem, users []dataiku.UserListItem) []string {
	logins := make(map[string]bool, len(users))
	members := map[string]bool{}
	for _, u := range users {
		logins[u.Login] = true
		for _, g := range u.Groups {
			members[g] = true
		}
	}

	orphans := []string{}
	for _, g := range groups {
		if testAccInstanceGroups[g.Name] {
			continue
		}
		match := testAccPerUserGroupName.FindStringSubmatch(g.Name)
		if match == nil {
			continue
		}
		if logins[match[1]] || members[g.Name] {
			continue
		}
		orphans = append(orphans, g.Name)
	}
	sort.Strings(orphans)
	return orphans
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

// Which Python interpreters exist is a property of the host, not of DSS, so
// the code environment test takes one the way it takes licence profiles.
//
// The default is PYTHON312 rather than nothing, because leaving it unset means
// DSS picks its own and that choice is frequently absent: DSS 15 falls back to
// python3.9 and Ubuntu 24.04, which the modules in this repository install,
// ships 3.12 only. The environment then fails to build while DSS reports it as
// created. Set DATAIKU_TEST_PYTHON_INTERPRETER if your instance has something
// else; `ls /usr/bin/python3*` on the host says what it has.
func testAccPythonInterpreter(t *testing.T) string {
	t.Helper()
	if v := os.Getenv("DATAIKU_TEST_PYTHON_INTERPRETER"); v != "" {
		return v
	}
	return "PYTHON312"
}

// Which licence profiles a DSS instance grants depends on its licence, so the
// two the user test switches between are configurable. The defaults match what
// a Dataiku trial licences; read /public/api/admin/licensing/status to find
// what yours does.
func testAccUserProfiles(t *testing.T) (string, string) {
	t.Helper()

	first := os.Getenv("DATAIKU_TEST_USER_PROFILE")
	second := os.Getenv("DATAIKU_TEST_USER_PROFILE_ALT")
	if first != "" && second != "" {
		return first, second
	}

	// Ask the instance rather than guessing. A profile its licence does not
	// define is stored and then breaks every later project create, user edit
	// and user delete, with no way back through the API or dsscli, so a
	// hardcoded default is a way to destroy the instance under test. This
	// suite did exactly that: it defaulted to DESIGNER, which an unlicensed
	// DSS 15 does not define.
	licensed := testAccLicensedProfiles(t)
	if len(licensed) == 0 {
		t.Skip("the instance reports no licensed user profiles, so there is none safe to use")
	}
	if first == "" {
		first = licensed[0]
	}
	if second == "" {
		second = licensed[len(licensed)-1]
	}
	return first, second
}

// testAccLicensedProfiles reads the profiles the target instance licences,
// sorted, from the same endpoint the provider checks before writing a user.
func testAccLicensedProfiles(t *testing.T) []string {
	t.Helper()

	host := os.Getenv("DATAIKU_HOST")
	key := os.Getenv("DATAIKU_API_KEY")
	if host == "" {
		t.Fatal("DATAIKU_HOST is unset; testAccSetup should have pointed this at the fake")
	}

	req, err := http.NewRequest(http.MethodGet, strings.TrimSuffix(host, "/")+"/public/api/admin/licensing/status", nil)
	if err != nil {
		t.Fatalf("building the licensing request failed: %s", err)
	}
	req.SetBasicAuth(key, "")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("reading the licensing status failed: %s", err)
	}
	defer resp.Body.Close()

	var status struct {
		Limits struct {
			LicensedProfiles map[string]any `json:"licensedProfiles"`
		} `json:"limits"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("decoding the licensing status failed: %s", err)
	}

	out := make([]string, 0, len(status.Limits.LicensedProfiles))
	for name := range status.Limits.LicensedProfiles {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
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
  # Deliberately not alphabetical. DSS sorts tags, so an ordered list model
  # fails here with ".tags[0]: was zulu, but now alpha".
  tags        = ["zulu", "alpha"]
}
`, key),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("dataiku_project.test", "id", key),
					resource.TestCheckResourceAttr("dataiku_project.test", "project_key", key),
					resource.TestCheckResourceAttr("dataiku_project.test", "name", "Acceptance test"),
					resource.TestCheckResourceAttr("dataiku_project.test", "owner", "admin"),
					resource.TestCheckResourceAttr("dataiku_project.test", "short_desc", "first"),
					resource.TestCheckResourceAttr("dataiku_project.test", "tags.#", "2"),
					resource.TestCheckTypeSetElemAttr("dataiku_project.test", "tags.*", "zulu"),
					resource.TestCheckTypeSetElemAttr("dataiku_project.test", "tags.*", "alpha"),
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
					resource.TestCheckTypeSetElemAttr("dataiku_project.test", "tags.*", "c"),
				),
			},
			{
				ResourceName:      "dataiku_project.test",
				ImportState:       true,
				ImportStateId:     key,
				ImportStateVerify: true,
				// project_folder_id is create-only and never reported by DSS.
				//
				// The clear_*_on_delete flags are deliberately not ignored.
				// They were, and that hid a real wart: DSS does not store
				// them, so import left them null and the first plan on an
				// untouched import showed three changes. ImportState seeds
				// them with their defaults now, and leaving them verified
				// here is what keeps that true.
				ImportStateVerifyIgnore: []string{"project_folder_id"},
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
			{
				ResourceName:      "dataiku_group.test",
				ImportState:       true,
				ImportStateId:     name,
				ImportStateVerify: true,
				// An imported group carries no abilities. The permissions map
				// tracks only the keys configuration names, because the set of
				// may* flags differs between DSS versions and editions, and an
				// import has no configuration to read. Write them yourself after
				// importing, using the data source to discover the names this
				// instance supports.
				ImportStateVerifyIgnore: []string{"permissions.%", "permissions.mayCreateProjects"},
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

// TestAccUserLeavesAGroupBehind is the regression test for the leak itself: a
// user created and destroyed through Terraform, and the group DSS attached
// still standing afterwards until the sweep takes it.
func TestAccUserLeavesAGroupBehind(t *testing.T) {
	fake := testAccSetup(t)
	if fake == nil {
		t.Skip("runs against the fake only: it asserts on what the instance holds after a destroy")
	}
	login := randName(t, "usr")
	profile, _ := testAccUserProfiles(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "dataiku_user" "test" {
  login        = %[1]q
  display_name = "Leaves a group behind"
  user_profile = %[2]q
}
`, login, profile),
				Check: resource.TestCheckResourceAttr("dataiku_user.test", "id", login),
			},
		},
	})

	// resource.Test destroyed the user on its way out. The group it caused is
	// still there, which is the whole defect.
	attached := fake.attachedGroups(login)
	if len(attached) != 1 {
		t.Fatalf("expected one group attached to %q, got %v", login, attached)
	}

	testAccSweepPerUserGroups(t)

	if got := fake.attachedGroups(login); len(got) != 0 {
		t.Errorf("the sweep left %v behind", got)
	}
	if got := fake.groupNames(); !slices.Equal(got, []string{"administrators", "data_team", "readers"}) {
		t.Errorf("after the sweep the instance holds %v, want only its own groups", got)
	}
}

// TestAccSweepPerUserGroupsSparesEverythingElse is the other half, and the
// more important half: five groups that look like leftovers to some degree,
// one of which may be deleted.
func TestAccSweepPerUserGroupsSparesEverythingElse(t *testing.T) {
	fake := testAccSetup(t)
	if fake == nil {
		t.Skip("runs against the fake only: it creates users and groups directly, without Terraform")
	}

	client, err := dataiku.NewClient(dataiku.Config{
		Host:   os.Getenv("DATAIKU_HOST"),
		APIKey: os.Getenv("DATAIKU_API_KEY"),
	})
	if err != nil {
		t.Fatalf("building a client failed: %s", err)
	}
	ctx := context.Background()
	profile, _ := testAccUserProfiles(t)

	// member:    still a user, still in the group DSS gave it.
	// lingering: still a user, but the membership was taken away, which is
	//            what happens as soon as a configuration names groups of its
	//            own.
	// adopted:   deleted user, but somebody else was put in its group.
	// orphan:    deleted user, nobody in the group. The only one to go.
	logins := map[string]string{}
	for _, kind := range []string{"member", "lingering", "adopted", "orphan"} {
		login := randName(t, "usr")
		logins[kind] = login
		req := dataiku.CreateUserRequest{
			Login:       login,
			DisplayName: kind,
			SourceType:  "LOCAL",
			UserProfile: profile,
		}
		if err := client.CreateUser(ctx, req); err != nil {
			t.Fatalf("creating the %s user failed: %s", kind, err)
		}
	}

	groupOf := map[string]string{}
	for kind, login := range logins {
		attached := fake.attachedGroups(login)
		if len(attached) != 1 {
			t.Fatalf("expected one group attached to the %s user, got %v", kind, attached)
		}
		groupOf[kind] = attached[0]
	}

	// A group of the suite's own making, with no members and no user behind
	// its name, to prove the sweep goes by the shape of the name rather than
	// by looking abandoned.
	plain := randName(t, "grp")
	if err := client.CreateGroup(ctx, dataiku.CreateGroupRequest{Name: plain, SourceType: "LOCAL"}); err != nil {
		t.Fatalf("creating the plain group failed: %s", err)
	}

	err = client.UpdateUser(ctx, logins["lingering"], func(u map[string]any) {
		u["groups"] = []any{}
	})
	if err != nil {
		t.Fatalf("emptying the lingering user's groups failed: %s", err)
	}
	err = client.UpdateUser(ctx, logins["member"], func(u map[string]any) {
		u["groups"] = []any{groupOf["member"], groupOf["adopted"]}
	})
	if err != nil {
		t.Fatalf("adopting the other user's group failed: %s", err)
	}
	for _, kind := range []string{"adopted", "orphan"} {
		if err := client.DeleteUser(ctx, logins[kind]); err != nil {
			t.Fatalf("deleting the %s user failed: %s", kind, err)
		}
	}

	testAccSweepPerUserGroups(t)

	want := []string{
		"administrators",
		"data_team",
		"readers",
		plain,
		groupOf["member"],
		groupOf["lingering"],
		groupOf["adopted"],
	}
	sort.Strings(want)
	if got := fake.groupNames(); !slices.Equal(got, want) {
		t.Errorf("after the sweep the instance holds %v, want %v", got, want)
	}
}

// TestAccSetupRegistersTheSweep is what makes the fix a fix rather than a
// helper nobody calls: every test that goes through testAccSetup sweeps when
// it finishes, whether or not it remembered to.
//
// A test cannot watch its own cleanup run, so the work happens in a subtest
// and the assertion in its parent, after the subtest and its cleanups are
// done. The fake outlives its server, so it can still be read.
func TestAccSetupRegistersTheSweep(t *testing.T) {
	var fake *fakeDSS
	var login string

	t.Run("creates and deletes a user", func(t *testing.T) {
		fake = testAccSetup(t)
		if fake == nil {
			t.Skip("runs against the fake only: it asserts on what the instance holds")
		}

		client, err := dataiku.NewClient(dataiku.Config{
			Host:   os.Getenv("DATAIKU_HOST"),
			APIKey: os.Getenv("DATAIKU_API_KEY"),
		})
		if err != nil {
			t.Fatalf("building a client failed: %s", err)
		}

		ctx := context.Background()
		profile, _ := testAccUserProfiles(t)
		login = randName(t, "usr")
		req := dataiku.CreateUserRequest{
			Login:       login,
			DisplayName: "Swept on cleanup",
			SourceType:  "LOCAL",
			UserProfile: profile,
		}
		if err := client.CreateUser(ctx, req); err != nil {
			t.Fatalf("creating the user failed: %s", err)
		}
		if err := client.DeleteUser(ctx, login); err != nil {
			t.Fatalf("deleting the user failed: %s", err)
		}
		if got := fake.attachedGroups(login); len(got) != 1 {
			t.Fatalf("expected one group attached to %q, got %v", login, got)
		}
	})

	if fake == nil {
		t.Skip("the subtest did not run against the fake")
	}
	if got := fake.attachedGroups(login); len(got) != 0 {
		t.Errorf("finishing a test left %v behind, so nothing is sweeping", got)
	}
}

// TestAccSweepPerUserGroupsDoesNotFailTheSuite pins the other half of the
// cleanup contract. The sweep runs after every test, so a sweep that reports
// its own failures turns an unreachable instance into a suite full of red
// tests that have nothing wrong with them.
func TestAccSweepPerUserGroupsDoesNotFailTheSuite(t *testing.T) {
	refuses := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	t.Cleanup(refuses.Close)

	t.Setenv("DATAIKU_HOST", refuses.URL)
	t.Setenv("DATAIKU_API_KEY", "a-key-that-may-not-list-groups")

	// Anything the sweep reports rather than logs lands on this test.
	testAccSweepPerUserGroups(t)
}

// TestAccOrphanSelectorSparesNamedInstanceGroups covers the one guard the
// sweep's end to end tests cannot reach. On a stock instance no group of DSS's
// own has three dash-separated segments, so the shape check alone keeps the
// sweep away from them and the name list behind it never fires. It exists for
// the instance that does have one, and the only way to stand that up is to say
// so directly.
func TestAccOrphanSelectorSparesNamedInstanceGroups(t *testing.T) {
	saved := testAccInstanceGroups
	t.Cleanup(func() { testAccInstanceGroups = saved })
	testAccInstanceGroups = map[string]bool{"data-team-eu-west1": true}

	groups := []dataiku.GroupListItem{
		{Name: "data-team-eu-west1"},
		{Name: "tfacc_usr_gone-admin-pzgpgtih"},
	}
	want := []string{"tfacc_usr_gone-admin-pzgpgtih"}
	if got := testAccOrphanedPerUserGroups(groups, nil); !slices.Equal(got, want) {
		t.Errorf("the selector chose %v, want %v", got, want)
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
			{
				ResourceName:      "dataiku_connection.test",
				ImportState:       true,
				ImportStateId:     name,
				ImportStateVerify: true,
				// DSS redacts secrets when reading a connection back, so the
				// imported params cannot match what was written. Import reads the
				// redacted form in once as a starting point, which is the
				// documented behaviour rather than a gap in the test.
				ImportStateVerifyIgnore: []string{"params_json"},
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
			{
				ResourceName:      "dataiku_project_permissions.test",
				ImportState:       true,
				ImportStateId:     key,
				ImportStateVerify: true,
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
			{
				ResourceName:      "dataiku_project_variables.test",
				ImportState:       true,
				ImportStateId:     key,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccDataSources(t *testing.T) {
	testAccSetup(t)
	key := randProjectKey(t)
	group := randName(t, "grp")
	login := randName(t, "usr")
	profile, _ := testAccUserProfiles(t)

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
  user_profile = %[4]q
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
`, key, group, login, profile),
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

// Regression: DSS registers a code environment and then builds its virtualenv,
// and the create call returns after the first step. When the build fails there
// is no environment, and reads answer 500 about a missing desc.json, which
// describes a symptom several steps downstream of the cause. The provider used
// to pass that through as "was created but applying its settings failed",
// telling the practitioner the opposite of what happened.
//
// Asking for an interpreter the host does not have is how this shows up in
// practice: a DSS 15 instance on Ubuntu 24.04 defaults to python3.9 on a host
// that ships 3.12 only.
func TestAccCodeEnvBuildFailureReportsTheCause(t *testing.T) {
	fake := testAccSetup(t)
	if fake == nil {
		t.Skip("runs against the fake only: it needs an interpreter the instance is known not to have")
	}
	name := randName(t, "env")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "dataiku_code_env" "broken" {
  name               = %[1]q
  lang               = "PYTHON"
  python_interpreter = "PYTHON34"
}
`, name),
				// The message must name the real failure, quote the build log
				// DSS kept, and say what to do about it. Terraform hard-wraps
				// diagnostics, so match fragments short enough to survive it.
				ExpectError: regexp.MustCompile(`(?s)failed to build it.*build log.*python3\.4.*python_interpreter`),
			},
		},
	})
}

func TestAccCodeEnv(t *testing.T) {
	testAccSetup(t)
	name := randName(t, "env")
	interpreter := testAccPythonInterpreter(t)
	build := randName(t, "build")
	exec := randName(t, "exec")

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

  # Required in practice. Left unset DSS picks an interpreter the
  # host may not have, and the build fails while DSS reports success.
  python_interpreter = %[2]q

  packages                   = "scikit-learn==1.5.0"
  install_packages_on_change = false
}
`, name, interpreter),
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

  # Required in practice. Left unset DSS picks an interpreter the
  # host may not have, and the build fails while DSS reports success.
  python_interpreter = %[2]q

  packages                   = "scikit-learn==1.5.0\npandas==2.2.2"
  install_jupyter_support    = true
  usable_by_all              = false
  install_packages_on_change = false
}
`, name, interpreter),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("dataiku_code_env.test", "packages", "scikit-learn==1.5.0\npandas==2.2.2"),
					resource.TestCheckResourceAttr("dataiku_code_env.test", "install_jupyter_support", "true"),
					resource.TestCheckResourceAttr("dataiku_code_env.test", "usable_by_all", "false"),
				),
			},
			{
				// The container selection. A new environment builds for every
				// configuration; this narrows it to one, which is the state DSS
				// calls "Selected container configurations": the flag off and the
				// list non-empty.
				Config: fmt.Sprintf(`
resource "dataiku_container_image_build_config" "build" {
  name = %[3]q

  settings_json = jsonencode({
    baseImageType    = "EXEC"
    imageBuilderType = "DOCKER"
  })
}

resource "dataiku_container_execution_config" "exec" {
  name = %[4]q

  settings_json = jsonencode({
    type             = "KUBERNETES"
    imageBuildConfig = dataiku_container_image_build_config.build.name
    usableBy         = "ALL"
    allowedGroups    = []
  })
}

resource "dataiku_code_env" "test" {
  name = %[1]q
  lang = "PYTHON"

  # Required in practice. Left unset DSS picks an interpreter the
  # host may not have, and the build fails while DSS reports success.
  python_interpreter = %[2]q

  packages                   = "scikit-learn==1.5.0\npandas==2.2.2"
  install_jupyter_support    = true
  usable_by_all              = false
  install_packages_on_change = false

  all_container_configs = false
  container_configs     = [dataiku_container_execution_config.exec.name]

  # The Spark pair defaults to false with an empty list, so turning the flag
  # on is the change that proves the write lands rather than matching what
  # was already there.
  all_spark_kubernetes_configs = true
}
`, name, interpreter, build, exec),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("dataiku_code_env.test", "all_container_configs", "false"),
					resource.TestCheckResourceAttr("dataiku_code_env.test", "container_configs.#", "1"),
					resource.TestCheckTypeSetElemAttr("dataiku_code_env.test", "container_configs.*", exec),
					resource.TestCheckResourceAttr("dataiku_code_env.test", "all_spark_kubernetes_configs", "true"),
					resource.TestCheckResourceAttr("dataiku_code_env.test", "spark_kubernetes_configs.#", "0"),
					// usable_by_all is stored in the same two places and written the
					// same way, so this also catches a write that replaced the
					// document instead of editing it.
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
	// The fake accepts any trigger, so this only needs guarding when it is
	// pointed at a real instance, where the licence decides.
	if testAccSetup(t) == nil && !testAccLicenseFeature(t, "temporalTriggerAllowed") {
		t.Skip("this instance does not licence temporal triggers")
	}
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
					resource.TestCheckTypeSetElemAttr("dataiku_scenario.test", "tags.*", "managed-by-terraform"),
				),
			},
			{
				// The JSON attributes are written and not read back, so a
				// second plan on the same configuration must be empty rather
				// than diffing against what DSS rewrote them to.
				Config:   project + testAccScenarioRenamedConfig,
				PlanOnly: true,
			},
			{
				ResourceName:      "dataiku_scenario.test",
				ImportState:       true,
				ImportStateId:     key + "/Nightly_build",
				ImportStateVerify: true,
				// DSS returns the whole scenario document merged with its own
				// defaults, so the triggers it reports are not byte-for-byte what
				// was sent. The id, name and project are what import has to get
				// right.
				ImportStateVerifyIgnore: []string{"triggers_json", "steps_json", "params_json", "reporters_json"},
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
	if !testAccLicenseFeature(t, "advancedScenarioStepsAllowed") {
		t.Skip("this instance does not licence advanced scenario steps, so a Python scenario cannot be created")
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

// TestAccUserPasswordWriteOnly checks the property that makes write-only
// arguments worth having: the secret reaches DSS but never reaches state.
func TestAccUserPasswordWriteOnly(t *testing.T) {
	testAccSetup(t)
	login := randName(t, "usr")
	profile, _ := testAccUserProfiles(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		// Write-only attributes are a Terraform 1.11 feature. On anything older
		// the framework rejects the configuration outright with "WriteOnly
		// Attribute Not Allowed", so this has to skip rather than fail: the
		// provider supports 1.8 for everything else.
		TerraformVersionChecks: []tfversion.TerraformVersionCheck{
			tfversion.SkipBelow(tfversion.Version1_11_0),
		},
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "dataiku_user" "test" {
  login        = %[1]q
  display_name = "Write-only"
  user_profile = %[2]q

  password_wo         = "first-secret-value"
  password_wo_version = "1"
}
`, login, profile),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("dataiku_user.test", "id", login),
					resource.TestCheckResourceAttr("dataiku_user.test", "password_wo_version", "1"),
					// The whole point: neither the write-only value nor the
					// legacy attribute may hold the secret.
					resource.TestCheckNoResourceAttr("dataiku_user.test", "password_wo"),
					resource.TestCheckNoResourceAttr("dataiku_user.test", "password"),
					checkStateHasNoSecret("dataiku_user.test", "first-secret-value"),
				),
			},
			{
				// Rotating means changing the marker; the value alone leaves
				// nothing for the provider to notice.
				Config: fmt.Sprintf(`
resource "dataiku_user" "test" {
  login        = %[1]q
  display_name = "Write-only"
  user_profile = %[2]q

  password_wo         = "second-secret-value"
  password_wo_version = "2"
}
`, login, profile),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("dataiku_user.test", "password_wo_version", "2"),
					resource.TestCheckNoResourceAttr("dataiku_user.test", "password_wo"),
					checkStateHasNoSecret("dataiku_user.test", "second-secret-value"),
				),
			},
		},
	})
}

func TestAccUserRejectsBothPasswordForms(t *testing.T) {
	testAccSetup(t)
	login := randName(t, "usr")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "dataiku_user" "test" {
  login    = %[1]q
  password = "plain"

  password_wo         = "write-only"
  password_wo_version = "1"
}
`, login),
				ExpectError: regexp.MustCompile(`(?s)Invalid Attribute Combination`),
			},
		},
	})
}

func TestAccUserWriteOnlyPasswordNeedsVersion(t *testing.T) {
	testAccSetup(t)
	login := randName(t, "usr")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				// Without the marker a rotation would silently never apply, so
				// the combination is rejected rather than accepted quietly.
				Config: fmt.Sprintf(`
resource "dataiku_user" "test" {
  login       = %[1]q
  password_wo = "no-version-marker"
}
`, login),
				ExpectError: regexp.MustCompile(`(?s)Invalid Attribute Combination`),
			},
		},
	})
}

// TestAccConnectionParamsWriteOnly does the same for connection credentials,
// and against a real database confirms the write-only parameters actually
// arrive rather than merely being accepted.
func TestAccConnectionParamsWriteOnly(t *testing.T) {
	testAccSetup(t)
	name := randName(t, "conn")

	pgHost := os.Getenv("DATAIKU_TEST_PG_HOST")
	checks := []resource.TestCheckFunc{
		resource.TestCheckResourceAttr("dataiku_connection.test", "params_json_wo_version", "1"),
		resource.TestCheckNoResourceAttr("dataiku_connection.test", "params_json_wo"),
	}
	if pgHost != "" {
		checks = append(checks, checkConnectionReachesDatabase(name))
	} else {
		pgHost = "db.example.com"
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		// Write-only attributes are a Terraform 1.11 feature. On anything older
		// the framework rejects the configuration outright with "WriteOnly
		// Attribute Not Allowed", so this has to skip rather than fail: the
		// provider supports 1.8 for everything else.
		TerraformVersionChecks: []tfversion.TerraformVersionCheck{
			tfversion.SkipBelow(tfversion.Version1_11_0),
		},
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "dataiku_connection" "test" {
  name = %[1]q
  type = "PostgreSQL"

  params_json_wo = jsonencode({
    host     = %[2]q
    port     = %[3]q
    db       = %[4]q
    user     = %[5]q
    password = %[6]q
  })
  params_json_wo_version = "1"
}
`, name, pgHost,
					envOrDefault("DATAIKU_TEST_PG_PORT", "5432"),
					envOrDefault("DATAIKU_TEST_PG_DB", "probe"),
					envOrDefault("DATAIKU_TEST_PG_USER", "dku"),
					envOrDefault("DATAIKU_TEST_PG_PASSWORD", "probe-pw")),
				Check: resource.ComposeAggregateTestCheckFunc(checks...),
			},
		},
	})
}

// checkStateHasNoSecret scans every attribute the resource stored, rather than
// one named attribute, and fails if the secret appears in any of them. That is
// the property write-only arguments are supposed to guarantee, and a check on a
// single attribute name would miss it leaking somewhere else.
func checkStateHasNoSecret(resourceName, secret string) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		rs, ok := state.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("%s is not in state", resourceName)
		}
		for key, value := range rs.Primary.Attributes {
			if strings.Contains(value, secret) {
				return fmt.Errorf(
					"the write-only secret leaked into state at %s.%s; write-only attributes must never be persisted",
					resourceName, key)
			}
		}
		return nil
	}
}

// A user with no licence profile is not a configuration mistake you find out
// about later: DSS accepts it, stores userProfile as null, and the instance
// then fails every project create, user edit and user delete, through the API
// and through dsscli alike. Reproduced on a fresh DSS 15.0.0 with no licence.
// The provider refuses at plan time instead.
func TestAccUserRequiresAProfile(t *testing.T) {
	testAccSetup(t)
	login := randName(t, "usr")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "dataiku_user" "test" {
  login        = %[1]q
  display_name = "No profile"
  password     = "An0ther-passw0rd!"
}
`, login),
				ExpectError: regexp.MustCompile(`user_profile is required`),
			},
		},
	})
}

// Sending a profile the instance does not licence is unrecoverable: DSS
// stores it, and every later project create, user edit and user delete fails
// inside its licence enforcement, through the API and through dsscli alike.
// Confirmed against a fresh DSS 15.0.0 that licences only NONE, where a user
// created with DESIGNER broke the instance. The provider checks before
// writing rather than finding out afterwards.
func TestAccUserRejectsAnUnlicensedProfile(t *testing.T) {
	testAccSetup(t)
	login := randName(t, "usr")

	// PlanOnly, so this runs against a real instance too. It used to skip
	// there, because the check lived in Create and a regression would have
	// written the profile and taken the instance with it. The check runs in
	// ModifyPlan now, so a plan is enough to exercise it and a plan writes
	// nothing even if it regresses.
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "dataiku_user" "test" {
  login        = %[1]q
  display_name = "Unlicensed"
  password     = "An0ther-passw0rd!"
  user_profile = "NOT_A_LICENSED_PROFILE"
}
`, login),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`not licensed on this instance`),
			},
		},
	})
}

// Moving an existing user onto an unlicensed profile breaks the instance the
// same way creating one does, and that path had no check at all: Update wrote
// userProfile straight through. Found by planning a DESIGNER user against an
// instance that licenses only NONE and watching Terraform plan it happily.
func TestAccUserRejectsMovingToAnUnlicensedProfile(t *testing.T) {
	testAccSetup(t)
	login := randName(t, "usr")
	profile, _ := testAccUserProfiles(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "dataiku_user" "test" {
  login        = %[1]q
  display_name = "Licensed to start with"
  password     = "An0ther-passw0rd!"
  user_profile = %[2]q
}
`, login, profile),
			},
			{
				// PlanOnly for the same reason as above: the refusal happens
				// while planning, so nothing reaches the instance.
				Config: fmt.Sprintf(`
resource "dataiku_user" "test" {
  login        = %[1]q
  display_name = "Licensed to start with"
  password     = "An0ther-passw0rd!"
  user_profile = "NOT_A_LICENSED_PROFILE"
}
`, login),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`not licensed on this instance`),
			},
		},
	})
}

// testAccLicenseFeature reports whether the target instance licences a named
// feature, from the same endpoint the provider reads before writing a user.
//
// Which features are on differs between instances, and not in the direction
// you would guess: one unlicensed DSS 15.0.0 allowed temporal triggers and
// refused custom Python scenario steps, and another of the same image did the
// opposite. Hardcoding either expectation makes the suite fail for a reason
// that has nothing to do with the provider, so the tests ask instead.
func testAccLicenseFeature(t *testing.T, name string) bool {
	t.Helper()

	host := os.Getenv("DATAIKU_HOST")
	if host == "" {
		t.Fatal("DATAIKU_HOST is unset; testAccSetup should have pointed this at the fake")
	}

	req, err := http.NewRequest(http.MethodGet, strings.TrimSuffix(host, "/")+"/public/api/admin/licensing/status", nil)
	if err != nil {
		t.Fatalf("building the licensing request failed: %s", err)
	}
	req.SetBasicAuth(os.Getenv("DATAIKU_API_KEY"), "")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("reading the licensing status failed: %s", err)
	}
	defer resp.Body.Close()

	var status struct {
		Features map[string]any `json:"features"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("decoding the licensing status failed: %s", err)
	}

	allowed, ok := status.Features[name].(bool)
	return ok && allowed
}
