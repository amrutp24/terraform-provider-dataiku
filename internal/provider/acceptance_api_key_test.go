package provider

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/amrutp24/terraform-provider-dataiku/internal/dataiku"
)

// testAccAdminClient talks to whatever the suite is running against with the
// key the suite itself uses.
func testAccAdminClient() (*dataiku.Client, error) {
	return dataiku.NewClient(dataiku.Config{
		Host:   os.Getenv("DATAIKU_HOST"),
		APIKey: os.Getenv("DATAIKU_API_KEY"),
	})
}

// apiKeyAuthenticates asks the instance whether secret is a key it accepts.
func apiKeyAuthenticates(secret string) (bool, error) {
	client, err := dataiku.NewClient(dataiku.Config{
		Host:   os.Getenv("DATAIKU_HOST"),
		APIKey: secret,
		// A refusal is the answer here, not something to wait out. Negative,
		// because the client reads zero as unset.
		MaxRetries: -1,
	})
	if err != nil {
		return false, err
	}
	err = client.CheckConnectivity(context.Background())
	if err == nil {
		return true, nil
	}
	if dataiku.IsUnauthorized(err) {
		return false, nil
	}
	return false, err
}

func apiKeyAttr(state *terraform.State, resourceName, attribute string) (string, error) {
	rs, ok := state.RootModule().Resources[resourceName]
	if !ok {
		return "", fmt.Errorf("%s is not in state", resourceName)
	}
	return rs.Primary.Attributes[attribute], nil
}

// checkAPIKeyWorks is the assertion the resource exists for: the secret in
// state is a key the instance accepts. Everything else about the resource can
// be right and the key still be useless if this is not.
func checkAPIKeyWorks(resourceName string) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		secret, err := apiKeyAttr(state, resourceName, "key")
		if err != nil {
			return err
		}
		if secret == "" {
			return fmt.Errorf("%s has no key in state", resourceName)
		}
		if strings.Trim(secret, "*") == "" {
			return fmt.Errorf("%s stored the mask DSS returns on a read, not the secret", resourceName)
		}
		ok, err := apiKeyAuthenticates(secret)
		if err != nil {
			return fmt.Errorf("checking the key in %s against the instance: %w", resourceName, err)
		}
		if !ok {
			return fmt.Errorf("the key in %s does not authenticate", resourceName)
		}
		return nil
	}
}

// rememberAPIKeyAttr copies an attribute out of state so a later step can
// compare against it.
func rememberAPIKeyAttr(resourceName, attribute string, into *string) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		value, err := apiKeyAttr(state, resourceName, attribute)
		*into = value
		return err
	}
}

func checkAPIKeyAttrIs(resourceName, attribute string, want *string, same bool) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		got, err := apiKeyAttr(state, resourceName, attribute)
		if err != nil {
			return err
		}
		if same && got != *want {
			return fmt.Errorf("%s.%s changed and should not have", resourceName, attribute)
		}
		if !same && got == *want {
			return fmt.Errorf("%s.%s did not change and should have", resourceName, attribute)
		}
		return nil
	}
}

func TestAccGlobalAPIKey(t *testing.T) {
	fake := testAccSetup(t)
	label := randName(t, "key")

	var id, secret string

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		// Deleting the resource has to revoke the key, not just forget it.
		CheckDestroy: func(_ *terraform.State) error {
			if secret == "" {
				return fmt.Errorf("the test never captured a secret to check")
			}
			ok, err := apiKeyAuthenticates(secret)
			if err != nil {
				return err
			}
			if ok {
				return fmt.Errorf("the key still authenticates after the resource was destroyed")
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "dataiku_global_api_key" "test" {
  label       = %[1]q
  description = "before"

  permissions = {
    mayCreateProjects  = false
    mayWriteUnsafeCode = true
  }
}
`, label),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("dataiku_global_api_key.test", "id"),
					resource.TestCheckResourceAttr("dataiku_global_api_key.test", "label", label),
					resource.TestCheckResourceAttr("dataiku_global_api_key.test", "description", "before"),
					resource.TestCheckResourceAttr("dataiku_global_api_key.test", "admin", "false"),
					// One default turned off and one turned on, so neither can
					// pass by DSS simply ignoring the map.
					resource.TestCheckResourceAttr("dataiku_global_api_key.test", "permissions.mayCreateProjects", "false"),
					resource.TestCheckResourceAttr("dataiku_global_api_key.test", "permissions.mayWriteUnsafeCode", "true"),
					resource.TestCheckNoResourceAttr("dataiku_global_api_key.test", "groups"),
					resource.TestCheckResourceAttr("dataiku_global_api_key.test", "lifetime_days", "0"),
					resource.TestCheckNoResourceAttr("dataiku_global_api_key.test", "expires_on"),
					checkAPIKeyWorks("dataiku_global_api_key.test"),
					rememberAPIKeyAttr("dataiku_global_api_key.test", "id", &id),
					rememberAPIKeyAttr("dataiku_global_api_key.test", "key", &secret),
				),
			},
			{
				// Drop the description, change the rest, and give it a lifetime.
				Config: fmt.Sprintf(`
resource "dataiku_global_api_key" "test" {
  label         = "%[1]s_renamed"
  admin         = true
  lifetime_days = 30

  permissions = {
    mayCreateProjects  = true
    mayWriteUnsafeCode = true
  }
}
`, label),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("dataiku_global_api_key.test", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("dataiku_global_api_key.test", "label", label+"_renamed"),
					resource.TestCheckNoResourceAttr("dataiku_global_api_key.test", "description"),
					resource.TestCheckResourceAttr("dataiku_global_api_key.test", "admin", "true"),
					resource.TestCheckResourceAttr("dataiku_global_api_key.test", "permissions.mayCreateProjects", "true"),
					resource.TestCheckResourceAttr("dataiku_global_api_key.test", "lifetime_days", "30"),
					resource.TestMatchResourceAttr("dataiku_global_api_key.test", "expires_on",
						regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$`)),
					// An update must leave the secret alone, in state and on the
					// instance both.
					checkAPIKeyAttrIs("dataiku_global_api_key.test", "id", &id, true),
					checkAPIKeyAttrIs("dataiku_global_api_key.test", "key", &secret, true),
					checkAPIKeyWorks("dataiku_global_api_key.test"),
					func(_ *terraform.State) error {
						if fake == nil {
							return nil
						}
						// DSS keeps nothing that is not in the PUT. createdOn is
						// a field this provider does not model, so it surviving
						// is the proof the whole document went back.
						doc, ok := fake.apiKey(id)
						if !ok {
							return fmt.Errorf("the fake has no key %s", id)
						}
						if created, _ := doc["createdOn"].(float64); created == 0 {
							return fmt.Errorf("the update wiped createdOn: the provider sent a partial document and DSS replaces, it does not merge")
						}
						if _, kept := doc["description"]; kept {
							return fmt.Errorf("the description was removed from configuration and is still on the key")
						}
						return nil
					},
				),
			},
			{
				ResourceName:      "dataiku_global_api_key.test",
				ImportState:       true,
				ImportStateIdFunc: func(_ *terraform.State) (string, error) { return id, nil },
				ImportStateVerify: true,
				// DSS gives the secret out once, so an imported key has none.
				// Abilities are tracked only for the names configuration
				// lists, and an import has no configuration.
				ImportStateVerifyIgnore: []string{
					"key", "permissions.%", "permissions.mayCreateProjects", "permissions.mayWriteUnsafeCode",
				},
			},
		},
	})
}

// A key takes its rights from its own flags or from groups, and DSS lets it
// move one way only. Adding groups is an edit that keeps the secret. Taking
// them away is refused by DSS, so the provider has to plan a new key.
func TestAccGlobalAPIKeyGroups(t *testing.T) {
	testAccSetup(t)
	label := randName(t, "key")
	group := randName(t, "grp")

	var secret string

	config := func(keyBody string) string {
		return fmt.Sprintf(`
resource "dataiku_group" "test" {
  name = %[1]q
}

resource "dataiku_global_api_key" "test" {
  label = %[2]q
%[3]s
}
`, group, label, keyBody)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config(`  admin = true`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("dataiku_global_api_key.test", "admin", "true"),
					resource.TestCheckNoResourceAttr("dataiku_global_api_key.test", "groups"),
					rememberAPIKeyAttr("dataiku_global_api_key.test", "key", &secret),
				),
			},
			{
				Config: config(`  groups = [dataiku_group.test.name]`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("dataiku_global_api_key.test", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("dataiku_global_api_key.test", "groups.#", "1"),
					resource.TestCheckTypeSetElemAttr("dataiku_global_api_key.test", "groups.*", group),
					// The key's own admin flag goes with its own rights.
					resource.TestCheckResourceAttr("dataiku_global_api_key.test", "admin", "false"),
					checkAPIKeyAttrIs("dataiku_global_api_key.test", "key", &secret, true),
					checkAPIKeyWorks("dataiku_global_api_key.test"),
				),
			},
			{
				// An empty set is still group-based, so this is an edit too.
				Config: config(`  groups = []`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("dataiku_global_api_key.test", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("dataiku_global_api_key.test", "groups.#", "0"),
					checkAPIKeyAttrIs("dataiku_global_api_key.test", "key", &secret, true),
				),
			},
			{
				Config: config(`  admin = true`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("dataiku_global_api_key.test", plancheck.ResourceActionReplace),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("dataiku_global_api_key.test", "groups"),
					resource.TestCheckResourceAttr("dataiku_global_api_key.test", "admin", "true"),
					checkAPIKeyAttrIs("dataiku_global_api_key.test", "key", &secret, false),
					checkAPIKeyWorks("dataiku_global_api_key.test"),
					// And the key it replaced is gone, not left behind working.
					func(_ *terraform.State) error {
						ok, err := apiKeyAuthenticates(secret)
						if err != nil {
							return err
						}
						if ok {
							return fmt.Errorf("the replaced key still authenticates")
						}
						return nil
					},
				),
			},
		},
	})
}

func TestAccGlobalAPIKeyRejectsGroupsWithItsOwnRights(t *testing.T) {
	testAccSetup(t)

	for name, body := range map[string]string{
		"admin":       `admin = true`,
		"permissions": `permissions = { mayCreateProjects = true }`,
	} {
		t.Run(name, func(t *testing.T) {
			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
				Steps: []resource.TestStep{
					{
						Config: fmt.Sprintf(`
resource "dataiku_global_api_key" "test" {
  label  = "never created"
  groups = ["readers"]
  %s
}
`, body),
						PlanOnly: true,
						// The validator's own words, not just the attribute
						// names. A plan-only step with no error fails anyway,
						// for having a non-empty plan, and that message prints
						// the plan, attribute names and all.
						ExpectError: regexp.MustCompile(`Invalid Attribute Combination`),
					},
				},
			})
		})
	}
}

// DSS answers 200 to an ability it has never heard of and stores nothing. The
// provider has to notice, say which name, and not leave a key behind that
// Terraform has no record of.
func TestAccGlobalAPIKeyRejectsAnUnknownAbility(t *testing.T) {
	testAccSetup(t)
	label := randName(t, "key")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "dataiku_global_api_key" "test" {
  label = %[1]q

  permissions = {
    mayDoSomethingInvented = true
  }
}
`, label),
				// Both halves matter. Without the provider's own check,
				// Terraform still fails, with "inconsistent result after
				// apply", and that message names the ability too.
				ExpectError: regexp.MustCompile(`(?s)Unknown ability.*mayDoSomethingInvented`),
			},
		},
	})

	client, err := testAccAdminClient()
	if err != nil {
		t.Fatal(err)
	}
	keys, err := client.ListGlobalAPIKeys(context.Background())
	if err != nil {
		t.Fatalf("listing keys to look for a leftover: %s", err)
	}
	for _, key := range keys {
		if key["label"] == label {
			t.Errorf("the refused key %v was left on the instance", key["id"])
		}
	}
}

// A key deleted behind Terraform's back answers 400, not 404. If the provider
// does not read that as "gone", every later plan fails on the refresh instead
// of offering to create the key again.
func TestAccGlobalAPIKeyRecreatedWhenDeletedOutsideTerraform(t *testing.T) {
	testAccSetup(t)
	label := randName(t, "key")

	var id string

	config := fmt.Sprintf(`
resource "dataiku_global_api_key" "test" {
  label = %[1]q
}
`, label)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check:  rememberAPIKeyAttr("dataiku_global_api_key.test", "id", &id),
			},
			{
				PreConfig: func() {
					client, err := testAccAdminClient()
					if err != nil {
						t.Fatal(err)
					}
					if err := client.DeleteGlobalAPIKey(context.Background(), id); err != nil {
						t.Fatalf("deleting the key out of band: %s", err)
					}
				},
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("dataiku_global_api_key.test", plancheck.ResourceActionCreate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					checkAPIKeyAttrIs("dataiku_global_api_key.test", "id", &id, false),
					checkAPIKeyWorks("dataiku_global_api_key.test"),
				),
			},
		},
	})
}

// The way in for a key that already exists: import it, plan clean against a
// matching configuration, then change it. An imported key has no secret,
// because DSS gives it out once, and it has to stay that way through an
// update rather than turn into the mask a read returns.
func TestAccGlobalAPIKeyImportedThenChanged(t *testing.T) {
	testAccSetup(t)
	label := randName(t, "key")

	client, err := testAccAdminClient()
	if err != nil {
		t.Fatal(err)
	}
	created, err := client.CreateGlobalAPIKey(context.Background(), map[string]any{
		"label": label, "description": "made outside terraform",
	})
	if err != nil {
		t.Fatalf("creating a key to import: %s", err)
	}
	id, _ := created["id"].(string)
	t.Cleanup(func() {
		if err := client.DeleteGlobalAPIKey(context.Background(), id); err != nil && !dataiku.IsNotFound(err) {
			t.Logf("could not remove the key %s made for this test: %s", id, err)
		}
	})

	config := func(description string) string {
		return fmt.Sprintf(`
resource "dataiku_global_api_key" "test" {
  label       = %[1]q
  description = %[2]q
}
`, label, description)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:             config("made outside terraform"),
				ResourceName:       "dataiku_global_api_key.test",
				ImportState:        true,
				ImportStateId:      id,
				ImportStatePersist: true,
			},
			{
				// The configuration matches what was imported, so there is
				// nothing to do. An import that needs an apply to settle is
				// a dirty import.
				Config: config("made outside terraform"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
			{
				Config: config("now managed"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("dataiku_global_api_key.test", "id", id),
					resource.TestCheckResourceAttr("dataiku_global_api_key.test", "description", "now managed"),
					resource.TestCheckNoResourceAttr("dataiku_global_api_key.test", "key"),
				),
			},
		},
	})
}

// The create call answers with expiresOn set to 0 whatever lifetime was asked
// for, and only a read has the real date. A provider that trusted the create
// response would record a key with a lifetime as one that never expires.
func TestAccGlobalAPIKeyWithALifetimeAndAUser(t *testing.T) {
	testAccSetup(t)
	label := randName(t, "key")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "dataiku_global_api_key" "test" {
  label                  = %[1]q
  lifetime_days          = 7
  user_for_impersonation = "admin"
}
`, label),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("dataiku_global_api_key.test", "lifetime_days", "7"),
					resource.TestMatchResourceAttr("dataiku_global_api_key.test", "expires_on",
						regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$`)),
					resource.TestCheckResourceAttr("dataiku_global_api_key.test", "user_for_impersonation", "admin"),
					checkAPIKeyWorks("dataiku_global_api_key.test"),
				),
			},
			{
				// Back to a key that does not expire and acts as nobody.
				Config: fmt.Sprintf(`
resource "dataiku_global_api_key" "test" {
  label = %[1]q
}
`, label),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("dataiku_global_api_key.test", "lifetime_days", "0"),
					resource.TestCheckNoResourceAttr("dataiku_global_api_key.test", "expires_on"),
					resource.TestCheckNoResourceAttr("dataiku_global_api_key.test", "user_for_impersonation"),
				),
			},
		},
	})
}

func TestAccGlobalAPIKeyRejectsANegativeLifetime(t *testing.T) {
	testAccSetup(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				// DSS takes -1, stores it, and the key never expires.
				Config: `
resource "dataiku_global_api_key" "test" {
  label         = "never created"
  lifetime_days = -1
}
`,
				PlanOnly: true,
				// Not just the attribute name: see the note in the test above.
				ExpectError: regexp.MustCompile(`must be at least 0`),
			},
		},
	})
}
