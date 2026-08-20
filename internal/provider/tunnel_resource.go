package provider

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/matf0r/terraform-provider-playit/internal/playit"
)

var (
	_ resource.Resource                = (*tunnelResource)(nil)
	_ resource.ResourceWithConfigure   = (*tunnelResource)(nil)
	_ resource.ResourceWithImportState = (*tunnelResource)(nil)
)

// NewTunnelResource returns the playit_tunnel resource.
func NewTunnelResource() resource.Resource { return &tunnelResource{} }

type tunnelResource struct {
	cfg *providerConfig
}

func (r *tunnelResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_tunnel"
}

func (r *tunnelResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	cfg, ok := req.ProviderData.(*providerConfig)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected provider data",
			fmt.Sprintf("Expected *providerConfig, got %T. This is a bug in the provider.", req.ProviderData),
		)
		return
	}
	r.cfg = cfg
}

func (r *tunnelResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A playit.gg tunnel: a public endpoint forwarding traffic to a local address " +
			"through a playit agent.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Tunnel identifier.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Human-readable name shown in the playit dashboard.",
				Optional:            true,
			},
			"tunnel_type": schema.StringAttribute{
				MarkdownDescription: "Game or protocol preset. Omit this for a custom tunnel; there is no " +
					"`custom` value. One of: `" + joinBackticked(playit.TunnelTypes) + "`.",
				Optional:      true,
				Validators:    []validatorString{stringvalidator.OneOf(playit.TunnelTypes...)},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"port_type": schema.StringAttribute{
				MarkdownDescription: "Transport to forward: `tcp`, `udp` or `both`.",
				Required:            true,
				Validators:          []validatorString{stringvalidator.OneOf(playit.PortTypes...)},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"port_count": schema.Int64Attribute{
				MarkdownDescription: "Number of consecutive ports to allocate. Defaults to 1.",
				Optional:            true,
				Computed:            true,
				Validators:          []validatorInt64{int64validator.Between(1, 65535)},
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"enabled": schema.BoolAttribute{
				MarkdownDescription: "Whether the tunnel accepts traffic. Note that the API does not report " +
					"this back, so it is tracked from configuration; use `active` to observe reachability.",
				Optional:      true,
				Computed:      true,
				Default:       booldefault.StaticBool(true),
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"firewall_id": schema.StringAttribute{
				MarkdownDescription: "Identifier of a firewall to attach to the tunnel.",
				Optional:            true,
			},
			"proxy_protocol": schema.StringAttribute{
				MarkdownDescription: "Prepend a PROXY protocol header to forwarded connections: `" +
					joinBackticked(playit.ProxyProtocols) + "`.",
				Optional:   true,
				Validators: []validatorString{stringvalidator.OneOf(playit.ProxyProtocols...)},
			},

			"origin": schema.SingleNestedAttribute{
				MarkdownDescription: "Where the tunnel forwards traffic.",
				Required:            true,
				Attributes: map[string]schema.Attribute{
					"type": schema.StringAttribute{
						MarkdownDescription: "Origin kind as the control plane resolved it: `agent` or " +
							"`managed`. This is reported, not configured -- it follows from `agent_id` " +
							"and `local_ip`. Setting only `local_ip` binds the tunnel to this account's " +
							"own agent, which the API then reports as `agent`.",
						Computed: true,
					},
					"agent_id": schema.StringAttribute{
						MarkdownDescription: "Agent to bind the tunnel to. Left unset, playit picks the " +
							"account's own agent and reports which one it chose. Changing it forces " +
							"replacement: the API refuses to move a tunnel between agents.",
						Optional: true,
						Computed: true,
						PlanModifiers: []planmodifier.String{
							stringplanmodifier.RequiresReplace(),
							stringplanmodifier.UseStateForUnknown(),
						},
					},
					"agent_name": schema.StringAttribute{
						MarkdownDescription: "Name of the bound agent.",
						Computed:            true,
					},
					"local_ip": schema.StringAttribute{
						MarkdownDescription: "Local address the agent forwards to, for example `127.0.0.1`. " +
							"Omit it, with `agent_id` set, for a managed origin. Adding or removing it " +
							"forces replacement, because it changes the kind of origin.",
						Optional:      true,
						PlanModifiers: []planmodifier.String{requiresReplaceOnOriginKindChange()},
					},
					"local_port": schema.Int64Attribute{
						MarkdownDescription: "Local port the agent forwards to.",
						Optional:            true,
						Validators:          []validatorInt64{int64validator.Between(1, 65535)},
					},
				},
			},

			"ratelimit": schema.SingleNestedAttribute{
				MarkdownDescription: "Throughput caps. Requires a playit premium subscription.",
				Optional:            true,
				Attributes: map[string]schema.Attribute{
					"bytes_per_second": schema.Int64Attribute{
						MarkdownDescription: "Maximum bytes per second.",
						Optional:            true,
						Validators:          []validatorInt64{int64validator.AtLeast(1)},
					},
					"packets_per_second": schema.Int64Attribute{
						MarkdownDescription: "Maximum packets per second.",
						Optional:            true,
						Validators:          []validatorInt64{int64validator.AtLeast(1)},
					},
				},
			},

			"alloc": schema.SingleNestedAttribute{
				MarkdownDescription: "How the public endpoint is allocated. Set exactly one member.\n\n" +
					"~> **This can cost money.** A dedicated IP or a reserved port allocation is a paid " +
					"playit feature, and applying a configuration that requests one will incur charges on " +
					"your account. Omit `alloc` entirely to use the free shared allocation.\n\n" +
					"Changing any part of this forces replacement; allocation is fixed at creation.",
				Optional:      true,
				PlanModifiers: []planmodifier.Object{objectplanmodifier.RequiresReplace()},
				Attributes: map[string]schema.Attribute{
					"dedicated_ip": schema.SingleNestedAttribute{
						MarkdownDescription: "Place the tunnel on a dedicated IP you already own.",
						Optional:            true,
						Attributes: map[string]schema.Attribute{
							"ip_hostname": schema.StringAttribute{
								MarkdownDescription: "Hostname of the dedicated IP.",
								Required:            true,
							},
							"port": schema.Int64Attribute{
								MarkdownDescription: "Specific public port to claim on that IP.",
								Optional:            true,
								Validators:          []validatorInt64{int64validator.Between(1, 65535)},
							},
						},
					},
					"port_allocation": schema.SingleNestedAttribute{
						MarkdownDescription: "Consume a port allocation already reserved on the account.",
						Optional:            true,
						Attributes: map[string]schema.Attribute{
							"alloc_id": schema.StringAttribute{
								MarkdownDescription: "Identifier of the port allocation.",
								Required:            true,
							},
						},
					},
					"region": schema.StringAttribute{
						MarkdownDescription: "Pin the tunnel to a region: `" + joinBackticked(playit.Regions) + "`.",
						Optional:            true,
						Validators:          []validatorString{stringvalidator.OneOf(playit.Regions...)},
					},
				},
			},

			"created_at":      computedString("When the tunnel was created, in RFC 3339."),
			"active":          computedBool("Whether playit currently considers the tunnel reachable. This is false whenever the agent is offline, independently of `enabled`."),
			"disabled_reason": computedString("Why playit disabled the tunnel, if it did."),
			"assigned_domain": computedString("Domain assigned to the tunnel."),
			"assigned_srv":    computedString("SRV record assigned to the tunnel, where the tunnel type publishes one."),
			"ip_hostname":     computedString("Hostname of the allocated public IP."),
			"tunnel_ip":       computedString("Public IP address serving the tunnel."),
			"region":          computedString("Region serving the tunnel."),
			"public_address":  computedString("Address to connect to: the SRV record when one exists, otherwise `assigned_domain:port_start`."),
			"port_start":      computedInt64("First allocated public port."),
			"port_end":        computedInt64("Last allocated public port."),
		},
	}
}

// requiresReplaceOnOriginKindChange forces replacement when a local address is
// added or removed, which is what distinguishes an agent origin from a managed
// one. Changing the address itself is an ordinary in-place update.
//
// Without this the change would be silently dropped: there is no endpoint that
// converts one kind of origin into the other, and /tunnels/update has nothing to
// send for a managed origin.
func requiresReplaceOnOriginKindChange() planmodifier.String {
	return stringplanmodifier.RequiresReplaceIf(
		func(_ context.Context, req planmodifier.StringRequest, resp *stringplanmodifier.RequiresReplaceIfFuncResponse) {
			resp.RequiresReplace = req.StateValue.IsNull() != req.PlanValue.IsNull()
		},
		"Forces replacement when a local address is added or removed.",
		"Forces replacement when a local address is added or removed.",
	)
}

// ---------------------------------------------------------------------------
// Create
// ---------------------------------------------------------------------------

func (r *tunnelResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan tunnelModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	createReq, diags := plan.expandCreate()
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	created, err := r.cfg.client.TunnelsCreate(ctx, createReq)
	if err != nil {
		resp.Diagnostics.Append(diagnoseError("Could not create playit tunnel", err))
		return
	}

	// Persist the identifier before waiting for anything.
	//
	// The tunnel exists on the account from this point on. If allocation never
	// settles, or the operator interrupts the apply, state must already know the
	// id -- otherwise the tunnel is orphaned: unmanaged, still billed when it
	// used a paid allocation, and invisible to terraform destroy.
	plan.ID = types.StringValue(created.ID)
	plan.Enabled = types.BoolValue(createReq.Enabled)
	plan.clearComputed()
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tunnel, err := r.waitForAllocation(ctx, created.ID)
	if err != nil {
		resp.Diagnostics.Append(diagnoseError(
			"playit tunnel was created but has no public address yet", err))
		return
	}

	plan.flatten(tunnel)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// waitForAllocation polls until the control plane assigns a public endpoint.
//
// Creation is asynchronous: /tunnels/create returns an id and nothing else, and
// the allocation stays pending for a short while. Returning early would leave
// every computed address null on the first apply.
func (r *tunnelResource) waitForAllocation(ctx context.Context, id string) (*playit.AccountTunnel, error) {
	deadline := time.Now().Add(r.cfg.createTimeout)
	backoff := time.Second

	for {
		tunnel, err := r.cfg.client.TunnelsGet(ctx, id)
		if err != nil {
			return nil, err
		}
		if tunnel == nil {
			return nil, fmt.Errorf("tunnel %s vanished before it was allocated", id)
		}

		switch {
		case tunnel.Alloc.Allocated != nil:
			return tunnel, nil
		case tunnel.Alloc.Disabled != nil:
			return nil, fmt.Errorf("playit refused to allocate the tunnel: %s", tunnel.Alloc.Disabled.Reason)
		}

		if time.Now().After(deadline) {
			return nil, fmt.Errorf(
				"timed out after %s waiting for a public address; the tunnel exists and is recorded in state",
				r.cfg.createTimeout)
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff):
		}

		backoff = time.Duration(float64(backoff) * 1.5)
		if backoff > 10*time.Second {
			backoff = 10 * time.Second
		}
	}
}

// ---------------------------------------------------------------------------
// Read
// ---------------------------------------------------------------------------

func (r *tunnelResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state tunnelModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tunnel, err := r.cfg.client.TunnelsGet(ctx, state.ID.ValueString())
	if err != nil {
		if playit.IsFail(err, "TunnelNotFound") {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.Append(diagnoseError("Could not read playit tunnel", err))
		return
	}
	if tunnel == nil {
		tflog.Debug(ctx, "playit tunnel no longer exists, dropping it from state",
			map[string]any{"id": state.ID.ValueString()})
		resp.State.RemoveResource(ctx)
		return
	}

	state.flatten(tunnel)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// ---------------------------------------------------------------------------
// Update
// ---------------------------------------------------------------------------

func (r *tunnelResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state tunnelModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	plan.ID = state.ID

	if err := r.applyChanges(ctx, id, &plan, &state); err != nil {
		// The six calls below are not transactional. Record what the server
		// actually holds before surfacing the failure, so state never reports a
		// configuration that was only partially applied.
		if tunnel, readErr := r.cfg.client.TunnelsGet(ctx, id); readErr == nil && tunnel != nil {
			observed := state
			observed.flatten(tunnel)
			resp.Diagnostics.Append(resp.State.Set(ctx, &observed)...)
		}
		resp.Diagnostics.Append(diagnoseError("Could not update playit tunnel", err))
		return
	}

	tunnel, err := r.cfg.client.TunnelsGet(ctx, id)
	if err != nil {
		resp.Diagnostics.Append(diagnoseError("playit tunnel was updated but could not be read back", err))
		return
	}
	if tunnel == nil {
		resp.Diagnostics.AddError(
			"playit tunnel disappeared during update",
			"The tunnel was updated but is no longer listed on the account.")
		return
	}

	plan.flatten(tunnel)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// applyChanges dispatches each changed group to its own endpoint.
//
// playit has no single update call; six single-purpose endpoints cover between
// them what one PATCH would. The order matters: configuration is applied before
// the tunnel is re-enabled, so a tunnel never serves traffic under a stale rate
// limit or PROXY protocol setting.
func (r *tunnelResource) applyChanges(ctx context.Context, id string, plan, state *tunnelModel) error {
	c := r.cfg.client

	// 1. Name.
	if !plan.Name.Equal(state.Name) {
		if err := c.TunnelsRename(ctx, playit.ReqTunnelsRename{
			TunnelID: id,
			Name:     plan.Name.ValueString(),
		}); err != nil {
			return err
		}
	}

	enabledChanged := !plan.Enabled.Equal(state.Enabled)

	// 2. Local address. /tunnels/update also carries the enabled flag, so a
	// combined change costs one call rather than two.
	if originAddressChanged(plan.Origin, state.Origin) {
		// agent_id is echoed back unchanged on purpose: the field is accepted but
		// any actual change is rejected, and moving agents forces replacement.
		var agentID *string
		if state.Origin != nil {
			agentID = stringPtr(state.Origin.AgentID)
		}
		if err := c.TunnelsUpdate(ctx, playit.ReqTunnelsUpdate{
			TunnelID:  id,
			LocalIP:   plan.Origin.LocalIP.ValueString(),
			LocalPort: uint16Ptr(plan.Origin.LocalPort),
			AgentID:   agentID,
			Enabled:   boolValue(plan.Enabled, true),
		}); err != nil {
			return err
		}
		enabledChanged = false
	}

	// 3. Firewall.
	if !plan.FirewallID.Equal(state.FirewallID) {
		if err := c.TunnelsFirewallAssign(ctx, playit.ReqTunnelsFirewallAssign{
			TunnelID:   id,
			FirewallID: stringPtr(plan.FirewallID),
		}); err != nil {
			return err
		}
	}

	// 4. PROXY protocol.
	if !plan.ProxyProtocol.Equal(state.ProxyProtocol) {
		if err := c.TunnelsProxySet(ctx, playit.ReqTunnelsProxySet{
			TunnelID:      id,
			ProxyProtocol: proxyProtocolPtr(plan.ProxyProtocol),
		}); err != nil {
			return err
		}
	}

	// 5. Rate limits.
	if ratelimitChanged(plan.Ratelimit, state.Ratelimit) {
		req := playit.ReqTunnelsRatelimit{TunnelID: id}
		if plan.Ratelimit != nil {
			req.BytesPerSecond = uint32Ptr(plan.Ratelimit.BytesPerSecond)
			req.PacketsPerSecond = uint32Ptr(plan.Ratelimit.PacketsPerSecond)
		}
		if err := c.TunnelsRatelimit(ctx, req); err != nil {
			return err
		}
	}

	// 6. Enabled, when nothing above already carried it.
	if enabledChanged {
		if err := c.TunnelsEnable(ctx, playit.ReqTunnelsEnable{
			TunnelID: id,
			Enabled:  boolValue(plan.Enabled, true),
		}); err != nil {
			return err
		}
	}

	return nil
}

// originAddressChanged reports whether the local address needs pushing.
//
// A managed origin has no local address at all, so there is nothing to send.
func originAddressChanged(plan, state *originModel) bool {
	if plan == nil {
		return false
	}
	if plan.LocalIP.IsNull() || plan.LocalIP.ValueString() == "" {
		return false
	}
	if state == nil {
		return true
	}
	return !plan.LocalIP.Equal(state.LocalIP) || !plan.LocalPort.Equal(state.LocalPort)
}

func ratelimitChanged(plan, state *ratelimitModel) bool {
	switch {
	case plan == nil && state == nil:
		return false
	case plan == nil || state == nil:
		return true
	}
	return !plan.BytesPerSecond.Equal(state.BytesPerSecond) ||
		!plan.PacketsPerSecond.Equal(state.PacketsPerSecond)
}

// ---------------------------------------------------------------------------
// Delete and import
// ---------------------------------------------------------------------------

func (r *tunnelResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state tunnelModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.cfg.client.TunnelsDelete(ctx, playit.ReqTunnelsDelete{TunnelID: state.ID.ValueString()})
	// A tunnel that is already gone is the outcome we wanted.
	if err != nil && !playit.IsFail(err, "TunnelNotFound") {
		resp.Diagnostics.Append(diagnoseError("Could not delete playit tunnel", err))
	}
}

func (r *tunnelResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
