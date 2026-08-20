package provider

import (
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/matf0r/terraform-provider-playit/internal/playit"
)

func allocatedTunnel() *playit.AccountTunnel {
	return &playit.AccountTunnel{
		ID:        "tun-1",
		PortType:  playit.PortTypeTCP,
		PortCount: 1,
		CreatedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		Active:    true,
		Origin: &playit.TunnelOrigin{Agent: &playit.AssignedAgent{
			AgentID:   "agent-1",
			AgentName: "home",
			LocalIP:   "127.0.0.1",
		}},
		Alloc: playit.AccountTunnelAllocation{Allocated: &playit.TunnelAllocated{
			ID:             "alloc-1",
			IPHostname:     "ip.example",
			AssignedDomain: "x.playit.gg",
			TunnelIP:       "1.2.3.4",
			PortStart:      31000,
			PortEnd:        31001,
			Region:         "europe",
		}},
	}
}

// The API never echoes the alloc block back, so a read must leave it alone.
// Deriving it from the response would blank a configured value and show a
// spurious replacement on the next plan.
func TestFlattenPreservesAlloc(t *testing.T) {
	m := tunnelModel{
		Alloc: &allocModel{
			Region:      types.StringValue("europe"),
			DedicatedIP: nil,
		},
	}

	m.flatten(allocatedTunnel())

	if m.Alloc == nil {
		t.Fatal("alloc was dropped by flatten")
	}
	if m.Alloc.Region.ValueString() != "europe" {
		t.Errorf("alloc.region = %q, want europe", m.Alloc.Region.ValueString())
	}
}

// active reports observed reachability and is false whenever the agent is
// merely offline. Folding it into enabled would disable the tunnel on the next
// apply, taking a working server off the internet.
func TestFlattenDoesNotDeriveEnabledFromActive(t *testing.T) {
	tunnel := allocatedTunnel()
	tunnel.Active = false

	m := tunnelModel{Enabled: types.BoolValue(true)}
	m.flatten(tunnel)

	if !m.Enabled.ValueBool() {
		t.Error("enabled was turned off by an inactive tunnel")
	}
	if m.Active.ValueBool() {
		t.Error("active should reflect the API")
	}
}

// An imported tunnel has nothing in state to preserve.
func TestFlattenAssumesEnabledWhenStateIsEmpty(t *testing.T) {
	m := tunnelModel{Enabled: types.BoolNull()}
	m.flatten(allocatedTunnel())

	if !m.Enabled.ValueBool() {
		t.Error("an imported tunnel should be assumed enabled")
	}
}

// origin.type is read-only: the control plane resolves a default origin into a
// concrete agent one, so it always reports "agent" here. The resolved agent is
// absorbed into state, which is what makes agent_id useful after an apply.
func TestFlattenReportsResolvedOrigin(t *testing.T) {
	m := tunnelModel{Origin: &originModel{
		AgentID:   types.StringNull(),
		LocalIP:   types.StringValue("127.0.0.1"),
		LocalPort: types.Int64Null(),
	}}

	m.flatten(allocatedTunnel())

	if got := m.Origin.Type.ValueString(); got != playit.OriginAgent {
		t.Errorf("origin.type = %q, want %q", got, playit.OriginAgent)
	}
	if got := m.Origin.AgentID.ValueString(); got != "agent-1" {
		t.Errorf("origin.agent_id = %q, want agent-1", got)
	}
	if got := m.Origin.AgentName.ValueString(); got != "home" {
		t.Errorf("origin.agent_name = %q, want home", got)
	}
}

// An unset ratelimit block must stay unset; materialising an all-null object
// makes Terraform see a phantom change.
func TestFlattenLeavesUnsetRatelimitNil(t *testing.T) {
	m := tunnelModel{}
	m.flatten(allocatedTunnel())

	if m.Ratelimit != nil {
		t.Errorf("ratelimit = %+v, want nil", m.Ratelimit)
	}
}

func TestFlattenReadsRatelimitWhenSet(t *testing.T) {
	bytes := uint32(2048)
	tunnel := allocatedTunnel()
	tunnel.Ratelimit = playit.Ratelimit{BytesPerSecond: &bytes}

	m := tunnelModel{}
	m.flatten(tunnel)

	if m.Ratelimit == nil {
		t.Fatal("ratelimit was not read back")
	}
	if m.Ratelimit.BytesPerSecond.ValueInt64() != 2048 {
		t.Errorf("bytes_per_second = %d", m.Ratelimit.BytesPerSecond.ValueInt64())
	}
	if !m.Ratelimit.PacketsPerSecond.IsNull() {
		t.Error("packets_per_second should stay null")
	}
}

func TestPublicAddress(t *testing.T) {
	srv := "_minecraft._tcp.x.playit.gg"

	cases := []struct {
		name  string
		alloc *playit.TunnelAllocated
		want  string
	}{
		{
			name:  "falls back to domain and port",
			alloc: &playit.TunnelAllocated{AssignedDomain: "x.playit.gg", PortStart: 31000},
			want:  "x.playit.gg:31000",
		},
		{
			// An SRV record hides the port, which is what belongs in a server list.
			name:  "prefers the srv record",
			alloc: &playit.TunnelAllocated{AssignedDomain: "x.playit.gg", PortStart: 31000, AssignedSRV: &srv},
			want:  srv,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := publicAddress(tc.alloc); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// A pending allocation has no addresses to report; they must read as null
// rather than as empty strings.
func TestFlattenPendingAllocationLeavesAddressesNull(t *testing.T) {
	tunnel := allocatedTunnel()
	tunnel.Alloc = playit.AccountTunnelAllocation{Pending: true}

	m := tunnelModel{}
	m.flatten(tunnel)

	if !m.PublicAddress.IsNull() {
		t.Errorf("public_address = %q, want null", m.PublicAddress.ValueString())
	}
	if !m.AssignedDomain.IsNull() {
		t.Error("assigned_domain should be null while allocation is pending")
	}
}

func TestExpandOriginInfersVariant(t *testing.T) {
	cases := []struct {
		name string
		in   originModel
		want string
	}{
		{
			name: "local address only is a default origin",
			in:   originModel{AgentID: types.StringNull(), LocalIP: types.StringValue("127.0.0.1")},
			want: playit.OriginDefault,
		},
		{
			name: "agent plus local address is an agent origin",
			in:   originModel{AgentID: types.StringValue("a-1"), LocalIP: types.StringValue("127.0.0.1")},
			want: playit.OriginAgent,
		},
		{
			name: "agent without a local address is managed",
			in:   originModel{AgentID: types.StringValue("a-1"), LocalIP: types.StringNull()},
			want: playit.OriginManaged,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.in.LocalPort = types.Int64Null()
			if got := tc.in.variant(); got != tc.want {
				t.Errorf("variant = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestExpandAllocRejectsAmbiguity(t *testing.T) {
	a := &allocModel{
		Region:      types.StringValue("europe"),
		DedicatedIP: &allocDedicatedIPModel{IPHostname: types.StringValue("ip.example"), Port: types.Int64Null()},
	}
	if _, diags := a.expand(); !diags.HasError() {
		t.Error("expected an error when more than one alloc member is set")
	}

	empty := &allocModel{Region: types.StringNull()}
	if _, diags := empty.expand(); !diags.HasError() {
		t.Error("expected an error for an alloc block with nothing set")
	}
}
