package provider

import (
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/matf0r/terraform-provider-playit/internal/playit"
)

type tunnelModel struct {
	ID            types.String `tfsdk:"id"`
	Name          types.String `tfsdk:"name"`
	TunnelType    types.String `tfsdk:"tunnel_type"`
	Description   types.String `tfsdk:"description"`
	PortType      types.String `tfsdk:"port_type"`
	PortCount     types.Int64  `tfsdk:"port_count"`
	Enabled       types.Bool   `tfsdk:"enabled"`
	FirewallID    types.String `tfsdk:"firewall_id"`
	ProxyProtocol types.String `tfsdk:"proxy_protocol"`

	Origin    *originModel    `tfsdk:"origin"`
	Ratelimit *ratelimitModel `tfsdk:"ratelimit"`
	Alloc     *allocModel     `tfsdk:"alloc"`

	CreatedAt      types.String `tfsdk:"created_at"`
	Active         types.Bool   `tfsdk:"active"`
	DisabledReason types.String `tfsdk:"disabled_reason"`
	AssignedDomain types.String `tfsdk:"assigned_domain"`
	AssignedSRV    types.String `tfsdk:"assigned_srv"`
	IPHostname     types.String `tfsdk:"ip_hostname"`
	TunnelIP       types.String `tfsdk:"tunnel_ip"`
	PortStart      types.Int64  `tfsdk:"port_start"`
	PortEnd        types.Int64  `tfsdk:"port_end"`
	Region         types.String `tfsdk:"region"`
	PublicAddress  types.String `tfsdk:"public_address"`
}

type originModel struct {
	Type      types.String `tfsdk:"type"`
	AgentID   types.String `tfsdk:"agent_id"`
	AgentName types.String `tfsdk:"agent_name"`
	LocalIP   types.String `tfsdk:"local_ip"`
	LocalPort types.Int64  `tfsdk:"local_port"`
}

type ratelimitModel struct {
	BytesPerSecond   types.Int64 `tfsdk:"bytes_per_second"`
	PacketsPerSecond types.Int64 `tfsdk:"packets_per_second"`
}

type allocModel struct {
	DedicatedIP    *allocDedicatedIPModel `tfsdk:"dedicated_ip"`
	PortAllocation *allocPortModel        `tfsdk:"port_allocation"`
	Region         types.String           `tfsdk:"region"`
}

type allocDedicatedIPModel struct {
	IPHostname types.String `tfsdk:"ip_hostname"`
	Port       types.Int64  `tfsdk:"port"`
}

type allocPortModel struct {
	AllocID types.String `tfsdk:"alloc_id"`
}

// ---------------------------------------------------------------------------
// Model -> wire
// ---------------------------------------------------------------------------

func (m *tunnelModel) expandCreate(keyAgentID string) (playit.ReqTunnelsCreate, diag.Diagnostics) {
	var diags diag.Diagnostics

	req := playit.ReqTunnelsCreate{
		Name:          stringPtr(m.Name),
		PortType:      playit.PortType(m.PortType.ValueString()),
		PortCount:     uint16Value(m.PortCount, 1),
		Enabled:       boolValue(m.Enabled, true),
		FirewallID:    stringPtr(m.FirewallID),
		ProxyProtocol: proxyProtocolPtr(m.ProxyProtocol),
	}
	if tt := stringPtr(m.TunnelType); tt != nil {
		converted := playit.TunnelType(*tt)
		req.TunnelType = &converted
	} else {
		// A tunnel with no type is a custom one, and the API insists on a
		// description for those.
		req.TunnelDescription = stringPtr(m.Description)
		if req.TunnelDescription == nil {
			diags.AddAttributeError(pathDescription(), "Missing description",
				"A tunnel with no tunnel_type is a custom tunnel, and the API requires a description for it. "+
					"Set description, or set tunnel_type.")
		}
	}

	origin, originDiags := m.Origin.expand(keyAgentID)
	diags.Append(originDiags...)
	req.Origin = origin

	alloc, allocDiags := m.Alloc.expand()
	diags.Append(allocDiags...)
	req.Alloc = alloc

	return req, diags
}

// variant resolves which origin union member to send.
//
// There is no "default" case. The API models a default origin as "the account's
// default agent", which a self-managed agent is not; sending one from a
// self-managed key is rejected with InvalidAgentId. The provider therefore
// always names an agent explicitly, falling back to the one the secret key
// itself belongs to.
//
// An origin with no local address is managed; anything else is an agent origin.
func (o *originModel) variant() string {
	if o.LocalIP.IsNull() || o.LocalIP.IsUnknown() || o.LocalIP.ValueString() == "" {
		return playit.OriginManaged
	}
	return playit.OriginAgent
}

func (o *originModel) expand(keyAgentID string) (playit.TunnelOriginCreate, diag.Diagnostics) {
	var diags diag.Diagnostics
	var out playit.TunnelOriginCreate

	if o == nil {
		diags.AddError("Missing origin", "A tunnel needs an origin describing where traffic is forwarded.")
		return out, diags
	}

	// An explicit agent wins; otherwise bind to whichever agent the secret key
	// authenticates as.
	agentID := keyAgentID
	if configured := stringPtr(o.AgentID); configured != nil {
		agentID = *configured
	}

	if o.variant() == playit.OriginManaged {
		var managed *string
		if agentID != "" {
			managed = &agentID
		}
		out.Managed = &playit.AssignedManagedCreate{AgentID: managed}
		return out, diags
	}

	if agentID == "" {
		diags.AddAttributeError(pathOriginAgentID(), "Missing origin.agent_id",
			"No agent was configured and the provider could not determine which agent the secret key "+
				"belongs to. Set origin.agent_id explicitly.")
		return out, diags
	}

	out.Agent = &playit.AssignedAgentCreate{
		AgentID:   agentID,
		LocalIP:   o.LocalIP.ValueString(),
		LocalPort: uint16Ptr(o.LocalPort),
	}
	return out, diags
}

func (a *allocModel) expand() (*playit.TunnelCreateUseAllocation, diag.Diagnostics) {
	var diags diag.Diagnostics
	if a == nil {
		return nil, diags
	}

	set := 0
	out := &playit.TunnelCreateUseAllocation{}

	if a.DedicatedIP != nil {
		set++
		out.DedicatedIP = &playit.UseAllocDedicatedIP{
			IPHostname: a.DedicatedIP.IPHostname.ValueString(),
			Port:       uint16Ptr(a.DedicatedIP.Port),
		}
	}
	if a.PortAllocation != nil {
		set++
		out.PortAllocation = &playit.UseAllocPortAlloc{AllocID: a.PortAllocation.AllocID.ValueString()}
	}
	if !a.Region.IsNull() && !a.Region.IsUnknown() && a.Region.ValueString() != "" {
		set++
		out.Region = &playit.UseRegion{Region: playit.PlayitNetwork(a.Region.ValueString())}
	}

	switch set {
	case 0:
		diags.AddAttributeError(pathAlloc(), "Empty alloc block",
			"Set exactly one of alloc.dedicated_ip, alloc.port_allocation or alloc.region, or remove the alloc block entirely.")
		return nil, diags
	case 1:
		return out, diags
	default:
		diags.AddAttributeError(pathAlloc(), "Conflicting alloc block",
			"alloc accepts exactly one of dedicated_ip, port_allocation or region.")
		return nil, diags
	}
}

// ---------------------------------------------------------------------------
// Wire -> model
// ---------------------------------------------------------------------------

// clearComputed nulls every server-derived attribute so the model can be written
// to state before those values are known. Terraform cannot persist unknowns.
func (m *tunnelModel) clearComputed() {
	m.CreatedAt = types.StringNull()
	m.Active = types.BoolNull()
	m.DisabledReason = types.StringNull()
	m.AssignedDomain = types.StringNull()
	m.AssignedSRV = types.StringNull()
	m.IPHostname = types.StringNull()
	m.TunnelIP = types.StringNull()
	m.PortStart = types.Int64Null()
	m.PortEnd = types.Int64Null()
	m.Region = types.StringNull()
	m.PublicAddress = types.StringNull()
	if m.PortCount.IsUnknown() {
		m.PortCount = types.Int64Null()
	}
	if m.Origin != nil {
		m.Origin.Type = types.StringNull()
		if m.Origin.AgentID.IsUnknown() {
			m.Origin.AgentID = types.StringNull()
		}
		m.Origin.AgentName = types.StringNull()
	}
}

// flatten copies server state onto the model.
//
// Two fields are deliberately preserved rather than overwritten:
//
//   - alloc and description have no read counterpart at all; the API never
//     echoes either back.
//   - enabled is the desired flag, which the legacy read model does not carry.
//     Only "active" comes back, and that is false whenever the agent is merely
//     offline. Conflating them would disable tunnels on the next apply.
func (m *tunnelModel) flatten(t *playit.AccountTunnel) {
	m.ID = types.StringValue(t.ID)
	m.Name = stringOrNull(t.Name)
	m.PortType = types.StringValue(string(t.PortType))
	m.PortCount = types.Int64Value(int64(t.PortCount))
	m.FirewallID = stringOrNull(t.FirewallID)
	m.CreatedAt = types.StringValue(t.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"))
	m.Active = types.BoolValue(t.Active)
	m.DisabledReason = stringOrNull(t.DisabledReason)

	if t.TunnelType != nil {
		m.TunnelType = types.StringValue(string(*t.TunnelType))
	} else {
		m.TunnelType = types.StringNull()
	}

	if t.ProxyProtocol != nil {
		m.ProxyProtocol = types.StringValue(string(*t.ProxyProtocol))
	} else {
		m.ProxyProtocol = types.StringNull()
	}

	// A tunnel that exists is presumed enabled when state has nothing to say,
	// which is the case immediately after an import.
	if m.Enabled.IsNull() || m.Enabled.IsUnknown() {
		m.Enabled = types.BoolValue(true)
	}

	m.flattenOrigin(t.Origin)
	m.flattenRatelimit(t.Ratelimit)
	m.flattenAllocation(t)
}

func (m *tunnelModel) flattenOrigin(o *playit.TunnelOrigin) {
	next := &originModel{
		Type:      types.StringNull(),
		AgentID:   types.StringNull(),
		AgentName: types.StringNull(),
		LocalIP:   types.StringNull(),
		LocalPort: types.Int64Null(),
	}

	switch {
	case o == nil:
		// Keep whatever the configuration asked for; there is nothing to learn.
		if m.Origin != nil {
			next = m.Origin
		}
		m.Origin = next
		return

	case o.Agent != nil:
		next.Type = types.StringValue(playit.OriginAgent)
		next.AgentID = types.StringValue(o.Agent.AgentID)
		next.AgentName = types.StringValue(o.Agent.AgentName)
		next.LocalIP = types.StringValue(o.Agent.LocalIP)
		next.LocalPort = int64OrNull16(o.Agent.LocalPort)

	case o.Managed != nil:
		next.Type = types.StringValue(playit.OriginManaged)
		next.AgentID = types.StringValue(o.Managed.AgentID)
		next.AgentName = types.StringValue(o.Managed.AgentName)
	}

	m.Origin = next
}

func (m *tunnelModel) flattenRatelimit(r playit.Ratelimit) {
	if r.BytesPerSecond == nil && r.PacketsPerSecond == nil {
		// An unset block must stay unset, or Terraform sees a phantom object.
		if m.Ratelimit != nil {
			m.Ratelimit = &ratelimitModel{
				BytesPerSecond:   types.Int64Null(),
				PacketsPerSecond: types.Int64Null(),
			}
		}
		return
	}
	m.Ratelimit = &ratelimitModel{
		BytesPerSecond:   int64OrNull32(r.BytesPerSecond),
		PacketsPerSecond: int64OrNull32(r.PacketsPerSecond),
	}
}

func (m *tunnelModel) flattenAllocation(t *playit.AccountTunnel) {
	region := types.StringNull()
	if t.Region != nil {
		region = types.StringValue(string(*t.Region))
	}

	alloc := t.Alloc.Allocated
	if alloc == nil {
		m.AssignedDomain = types.StringNull()
		m.AssignedSRV = types.StringNull()
		m.IPHostname = types.StringNull()
		m.TunnelIP = types.StringNull()
		m.PortStart = types.Int64Null()
		m.PortEnd = types.Int64Null()
		m.PublicAddress = types.StringNull()
		m.Region = region
		return
	}

	m.AssignedDomain = types.StringValue(alloc.AssignedDomain)
	m.AssignedSRV = stringOrNull(alloc.AssignedSRV)
	m.IPHostname = types.StringValue(alloc.IPHostname)
	m.TunnelIP = types.StringValue(alloc.TunnelIP)
	m.PortStart = types.Int64Value(int64(alloc.PortStart))
	m.PortEnd = types.Int64Value(int64(alloc.PortEnd))
	m.PublicAddress = types.StringValue(publicAddress(alloc))

	if region.IsNull() && alloc.Region != "" {
		region = types.StringValue(string(alloc.Region))
	}
	m.Region = region
}

// publicAddress renders the address a player would actually connect to.
//
// An SRV record, where the service publishes one, hides the port entirely; that
// is what belongs in a server list, so it wins when present.
func publicAddress(a *playit.TunnelAllocated) string {
	if a.AssignedSRV != nil && *a.AssignedSRV != "" {
		return *a.AssignedSRV
	}
	return a.AssignedDomain + ":" + itoa(int64(a.PortStart))
}
