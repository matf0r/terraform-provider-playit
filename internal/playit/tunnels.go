package playit

import "context"

// TunnelsCreate creates a tunnel and returns its identifier.
//
// The response carries nothing but an id: allocation of the public endpoint is
// asynchronous, so callers must poll TunnelsGet until the allocation settles.
func (c *Client) TunnelsCreate(ctx context.Context, req ReqTunnelsCreate) (ObjectID, error) {
	return call[ObjectID](ctx, c, "/tunnels/create", req)
}

// TunnelsList returns the account's tunnels, optionally filtered server-side.
func (c *Client) TunnelsList(ctx context.Context, req ReqTunnelsList) (AccountTunnels, error) {
	return call[AccountTunnels](ctx, c, "/tunnels/list", req)
}

// TunnelsGet returns a single tunnel, or nil if it no longer exists.
//
// There is no /tunnels/get endpoint, but /tunnels/list accepts a tunnel_id
// filter, so this costs one request and does not enumerate the account.
func (c *Client) TunnelsGet(ctx context.Context, id string) (*AccountTunnel, error) {
	res, err := c.TunnelsList(ctx, ReqTunnelsList{TunnelID: &id})
	if err != nil {
		return nil, err
	}
	for i := range res.Tunnels {
		if res.Tunnels[i].ID == id {
			return &res.Tunnels[i], nil
		}
	}
	return nil, nil
}

// TunnelsUpdate changes a tunnel's origin address and enabled flag.
func (c *Client) TunnelsUpdate(ctx context.Context, req ReqTunnelsUpdate) error {
	_, err := call[struct{}](ctx, c, "/tunnels/update", req)
	return err
}

// TunnelsDelete removes a tunnel.
func (c *Client) TunnelsDelete(ctx context.Context, req ReqTunnelsDelete) error {
	_, err := call[struct{}](ctx, c, "/tunnels/delete", req)
	return err
}

// TunnelsRename changes a tunnel's name.
func (c *Client) TunnelsRename(ctx context.Context, req ReqTunnelsRename) error {
	_, err := call[struct{}](ctx, c, "/tunnels/rename", req)
	return err
}

// TunnelsEnable toggles a tunnel without touching any other field.
func (c *Client) TunnelsEnable(ctx context.Context, req ReqTunnelsEnable) error {
	_, err := call[struct{}](ctx, c, "/tunnels/enable", req)
	return err
}

// TunnelsRatelimit sets throughput caps.
func (c *Client) TunnelsRatelimit(ctx context.Context, req ReqTunnelsRatelimit) error {
	_, err := call[struct{}](ctx, c, "/tunnels/ratelimit", req)
	return err
}

// TunnelsProxySet sets or clears the PROXY protocol.
func (c *Client) TunnelsProxySet(ctx context.Context, req ReqTunnelsProxySet) error {
	_, err := call[struct{}](ctx, c, "/tunnels/proxy/set", req)
	return err
}

// TunnelsFirewallAssign attaches or detaches a firewall.
func (c *Client) TunnelsFirewallAssign(ctx context.Context, req ReqTunnelsFirewallAssign) error {
	_, err := call[struct{}](ctx, c, "/tunnels/firewall/assign", req)
	return err
}

// AgentsRunData reports which agent a secret key belongs to.
//
// It doubles as the credential probe at configure time, and supplies the agent
// the provider binds tunnels to when the configuration does not name one: a
// self-managed key cannot create a tunnel with a "default" origin, so an
// explicit agent id is always required.
func (c *Client) AgentsRunData(ctx context.Context) (AgentRunData, error) {
	return call[AgentRunData](ctx, c, "/agents/rundata", struct{}{})
}
