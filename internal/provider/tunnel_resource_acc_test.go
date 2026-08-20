package provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// These drive the provider through Terraform itself, against the in-process
// stub in stub_api_test.go. They need the terraform binary but no credentials
// and no network, so they set TF_ACC themselves rather than being opt-in: the
// paths they cover -- Create's polling loop, Read, Delete, and the plugin
// protocol round trip -- are not reachable from ordinary unit tests.
//
// The tests in tunnel_update_test.go still call applyChanges directly, because
// asserting the exact sequence of API calls is easier there than through a plan.

const resourceName = "playit_tunnel.test"

var errRejectedCredential = regexp.MustCompile(`rejected the secret key`)

var testAccProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"playit": providerserver.NewProtocol6WithError(New("test")()),
}

func stubConfig(stub *stubAPI, body string) string {
	return fmt.Sprintf(`
provider "playit" {
  secret_key = %q
  api_base   = %q
}
%s
`, stubSecret, stub.URL(), body)
}

const configBasic = `
resource "playit_tunnel" "test" {
  name        = "acc-minecraft"
  tunnel_type = "minecraft-java"
  port_type   = "tcp"

  origin = {
    local_ip   = "127.0.0.1"
    local_port = 25565
  }
}
`

const configUpdatedPort = `
resource "playit_tunnel" "test" {
  name        = "acc-minecraft"
  tunnel_type = "minecraft-java"
  port_type   = "tcp"

  origin = {
    local_ip   = "127.0.0.1"
    local_port = 25566
  }
}
`

const configRenamedAndThrottled = `
resource "playit_tunnel" "test" {
  name        = "acc-creative"
  tunnel_type = "minecraft-java"
  port_type   = "tcp"

  origin = {
    local_ip   = "10.0.0.2"
    local_port = 25566
  }

  ratelimit = {
    bytes_per_second = 1048576
  }
}
`

const configReplacedPortType = `
resource "playit_tunnel" "test" {
  name        = "acc-minecraft"
  tunnel_type = "minecraft-java"
  port_type   = "udp"

  origin = {
    local_ip   = "127.0.0.1"
    local_port = 25565
  }
}
`

func checkDestroy(stub *stubAPI) resource.TestCheckFunc {
	return func(*terraform.State) error {
		if n := stub.count(); n != 0 {
			return fmt.Errorf("destroy left %d tunnel(s) behind on the API", n)
		}
		return nil
	}
}

func TestAccTunnel_lifecycle(t *testing.T) {
	t.Setenv("TF_ACC", "1")
	stub := newStubAPI(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviderFactories,
		CheckDestroy:             checkDestroy(stub),
		Steps: []resource.TestStep{
			{
				// Create polls until the allocation settles; without that the
				// computed addresses would all be null on the first apply.
				Config: stubConfig(stub, configBasic),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(resourceName, "id"),
					resource.TestCheckResourceAttr(resourceName, "name", "acc-minecraft"),
					resource.TestCheckResourceAttr(resourceName, "tunnel_type", "minecraft-java"),
					resource.TestCheckResourceAttr(resourceName, "port_type", "tcp"),
					resource.TestCheckResourceAttr(resourceName, "port_count", "1"),
					resource.TestCheckResourceAttr(resourceName, "enabled", "true"),
					resource.TestCheckResourceAttr(resourceName, "active", "true"),
					resource.TestCheckResourceAttr(resourceName, "assigned_domain", stubDomain),
					resource.TestCheckResourceAttr(resourceName, "port_start", "31000"),
					resource.TestCheckResourceAttr(resourceName, "public_address", "demo.playit.gg:31000"),
					// The server resolves the default origin and reports the
					// agent it picked.
					resource.TestCheckResourceAttr(resourceName, "origin.type", "agent"),
					resource.TestCheckResourceAttr(resourceName, "origin.agent_id", stubAgentID),
					resource.TestCheckResourceAttr(resourceName, "origin.agent_name", stubAgentName),
					resource.TestCheckResourceAttr(resourceName, "origin.local_port", "25565"),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				// Changing the local port is an ordinary in-place update.
				Config: stubConfig(stub, configUpdatedPort),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(resourceName, plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.TestCheckResourceAttr(resourceName, "origin.local_port", "25566"),
			},
			{
				// Rename, re-address and throttle at once: three endpoints in
				// one apply.
				Config: stubConfig(stub, configRenamedAndThrottled),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(resourceName, plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "name", "acc-creative"),
					resource.TestCheckResourceAttr(resourceName, "origin.local_ip", "10.0.0.2"),
					resource.TestCheckResourceAttr(resourceName, "ratelimit.bytes_per_second", "1048576"),
				),
			},
			{
				// port_type appears in no mutation endpoint, so it must rebuild.
				Config: stubConfig(stub, configReplacedPortType),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(resourceName, plancheck.ResourceActionReplace),
					},
				},
				Check: resource.TestCheckResourceAttr(resourceName, "port_type", "udp"),
			},
		},
	})
}

// A tunnel deleted in the playit dashboard must come back as something to
// create, not as a silent success.
func TestAccTunnel_recreatedAfterOutOfBandDelete(t *testing.T) {
	t.Setenv("TF_ACC", "1")
	stub := newStubAPI(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviderFactories,
		CheckDestroy:             checkDestroy(stub),
		Steps: []resource.TestStep{
			{
				Config: stubConfig(stub, configBasic),
				Check:  resource.TestCheckResourceAttrSet(resourceName, "id"),
			},
			{
				// A refresh-only step: the tunnel vanishes behind Terraform's
				// back, so the refresh must drop it from state and leave
				// something to create.
				PreConfig: func() {
					stub.deleteOutOfBand()
				},
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

// A custom tunnel has no tunnel_type at all; there is no "custom" value to set.
func TestAccTunnel_customTunnelHasNoType(t *testing.T) {
	t.Setenv("TF_ACC", "1")
	stub := newStubAPI(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviderFactories,
		CheckDestroy:             checkDestroy(stub),
		Steps: []resource.TestStep{
			{
				Config: stubConfig(stub, `
resource "playit_tunnel" "test" {
  name        = "acc-syncthing"
  description = "file sync"
  port_type   = "both"
  port_count  = 2

  origin = {
    local_ip   = "192.168.1.20"
    local_port = 22000
  }
}
`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr(resourceName, "tunnel_type"),
					resource.TestCheckResourceAttr(resourceName, "description", "file sync"),
					resource.TestCheckResourceAttr(resourceName, "port_type", "both"),
					resource.TestCheckResourceAttr(resourceName, "port_count", "2"),
					resource.TestCheckResourceAttr(resourceName, "port_start", "31000"),
					// The range end is exclusive: two ports means 31000..31002.
					resource.TestCheckResourceAttr(resourceName, "port_end", "31002"),
				),
			},
		},
	})
}

// A managed origin carries an agent but no local address, and must not send
// /tunnels/update, which has nothing to say about it.
func TestAccTunnel_managedOrigin(t *testing.T) {
	t.Setenv("TF_ACC", "1")
	stub := newStubAPI(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviderFactories,
		CheckDestroy:             checkDestroy(stub),
		Steps: []resource.TestStep{
			{
				Config: stubConfig(stub, fmt.Sprintf(`
resource "playit_tunnel" "test" {
  name        = "acc-managed"
  description = "managed origin"
  port_type   = "tcp"

  origin = {
    agent_id = %q
  }
}
`, stubAgentID)),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "origin.type", "managed"),
					resource.TestCheckResourceAttr(resourceName, "origin.agent_id", stubAgentID),
					resource.TestCheckNoResourceAttr(resourceName, "origin.local_ip"),
				),
			},
		},
	})
}

// An invalid credential must fail at configure time, not part-way through an
// apply.
func TestAccProvider_rejectsBadCredential(t *testing.T) {
	t.Setenv("TF_ACC", "1")
	stub := newStubAPI(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
provider "playit" {
  secret_key = "wrong-key"
  api_base   = %q
}
%s
`, stub.URL(), configBasic),
				ExpectError: errRejectedCredential,
			},
		},
	})
}
