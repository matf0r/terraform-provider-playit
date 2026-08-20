package playit

import "time"

// PortType selects the transport a tunnel forwards.
type PortType string

const (
	PortTypeTCP  PortType = "tcp"
	PortTypeUDP  PortType = "udp"
	PortTypeBoth PortType = "both"
)

// PortTypes lists every accepted port_type, for schema validation.
var PortTypes = []string{string(PortTypeTCP), string(PortTypeUDP), string(PortTypeBoth)}

// TunnelType is the game/protocol preset applied to a tunnel.
//
// There is no "custom" member: a custom tunnel is one with no tunnel_type at
// all, which is why the field is a pointer everywhere.
//
// The set is open and the service adds to it, so the values below are examples
// rather than an exhaustive list -- a live account was seen using
// "left-4-dead-2", which appears in no published client. The provider does not
// validate against them; the API is the authority.
type TunnelType string

const (
	TunnelTypeMinecraftJava    TunnelType = "minecraft-java"
	TunnelTypeMinecraftBedrock TunnelType = "minecraft-bedrock"
	TunnelTypeValheim          TunnelType = "valheim"
	TunnelTypeTerraria         TunnelType = "terraria"
	TunnelTypeStarbound        TunnelType = "starbound"
	TunnelTypeRust             TunnelType = "rust"
	TunnelType7Days            TunnelType = "7days"
	TunnelTypeUnturned         TunnelType = "unturned"
	TunnelTypeHTTPS            TunnelType = "https"
)

// TunnelTypes lists known tunnel_type values, for documentation only. It is not
// used for validation, because it is not exhaustive.
var TunnelTypes = []string{
	string(TunnelTypeMinecraftJava),
	string(TunnelTypeMinecraftBedrock),
	string(TunnelTypeValheim),
	string(TunnelTypeTerraria),
	string(TunnelTypeStarbound),
	string(TunnelTypeRust),
	string(TunnelType7Days),
	string(TunnelTypeUnturned),
	string(TunnelTypeHTTPS),
}

// ProxyProtocol selects the PROXY protocol version prepended to forwarded
// connections.
type ProxyProtocol string

const (
	ProxyProtocolV1 ProxyProtocol = "proxy-protocol-v1"
	ProxyProtocolV2 ProxyProtocol = "proxy-protocol-v2"
)

// ProxyProtocols lists every accepted proxy_protocol, for schema validation.
var ProxyProtocols = []string{string(ProxyProtocolV1), string(ProxyProtocolV2)}

// PlayitNetwork identifies an edge region.
type PlayitNetwork string

// Regions lists the selectable regions. The reserved and test members the API
// defines internally are deliberately omitted.
var Regions = []string{
	"global",
	"north-america", "south-america", "europe", "asia", "india",
	"chile", "japan", "australia",
	"seattle-washington", "los-angeles-california", "denver-colorado",
	"dallas-texas", "chicago-illinois", "new-york",
	"united-kingdom", "germany", "sweden", "poland", "romania",
}

// Origin variant tags.
const (
	OriginDefault = "default"
	OriginAgent   = "agent"
	OriginManaged = "managed"
)

// OriginTypes lists every accepted origin type, for schema validation.
var OriginTypes = []string{OriginDefault, OriginAgent, OriginManaged}

// ---------------------------------------------------------------------------
// Requests
// ---------------------------------------------------------------------------

// Optional fields are pointers and carry no omitempty: the server models them as
// Option<T> and expects an explicit null, not an absent key.

// ReqTunnelsCreate is the body of POST /tunnels/create.
type ReqTunnelsCreate struct {
	Name       *string     `json:"name"`
	TunnelType *TunnelType `json:"tunnel_type"`
	// TunnelDescription is required when TunnelType is nil; the API answers
	// TunnelTypeRequiresDescription otherwise. It appears in no published
	// client and is not echoed back on read.
	TunnelDescription *string                    `json:"tunnel_description"`
	PortType          PortType                   `json:"port_type"`
	PortCount         uint16                     `json:"port_count"`
	Origin            TunnelOriginCreate         `json:"origin"`
	Enabled           bool                       `json:"enabled"`
	Alloc             *TunnelCreateUseAllocation `json:"alloc"`
	FirewallID        *string                    `json:"firewall_id"`
	ProxyProtocol     *ProxyProtocol             `json:"proxy_protocol"`
}

// ReqTunnelsList is the body of POST /tunnels/list.
//
// Both filters are applied server-side, so reading one tunnel does not require
// listing the account.
type ReqTunnelsList struct {
	TunnelID *string `json:"tunnel_id"`
	AgentID  *string `json:"agent_id"`
}

// ReqTunnelsUpdate is the body of POST /tunnels/update.
//
// AgentID is present because the server accepts the field, but changing it is
// rejected with ChangingAgentIdNotAllowed; the provider forces replacement
// instead of ever sending a different value here.
type ReqTunnelsUpdate struct {
	TunnelID  string  `json:"tunnel_id"`
	LocalIP   string  `json:"local_ip"`
	LocalPort *uint16 `json:"local_port"`
	AgentID   *string `json:"agent_id"`
	Enabled   bool    `json:"enabled"`
}

// ReqTunnelsDelete is the body of POST /tunnels/delete.
type ReqTunnelsDelete struct {
	TunnelID string `json:"tunnel_id"`
}

// ReqTunnelsRename is the body of POST /tunnels/rename.
type ReqTunnelsRename struct {
	TunnelID string `json:"tunnel_id"`
	Name     string `json:"name"`
}

// ReqTunnelsEnable is the body of POST /tunnels/enable.
type ReqTunnelsEnable struct {
	TunnelID string `json:"tunnel_id"`
	Enabled  bool   `json:"enabled"`
}

// ReqTunnelsRatelimit is the body of POST /tunnels/ratelimit.
type ReqTunnelsRatelimit struct {
	TunnelID         string  `json:"tunnel_id"`
	BytesPerSecond   *uint32 `json:"bytes_per_second"`
	PacketsPerSecond *uint32 `json:"packets_per_second"`
}

// ReqTunnelsProxySet is the body of POST /tunnels/proxy/set.
type ReqTunnelsProxySet struct {
	TunnelID      string         `json:"tunnel_id"`
	ProxyProtocol *ProxyProtocol `json:"proxy_protocol"`
}

// ReqTunnelsFirewallAssign is the body of POST /tunnels/firewall/assign.
type ReqTunnelsFirewallAssign struct {
	TunnelID   string  `json:"tunnel_id"`
	FirewallID *string `json:"firewall_id"`
}

// ---------------------------------------------------------------------------
// Origin
// ---------------------------------------------------------------------------

// TunnelOriginCreate is the create-time origin union. Exactly one field is set.
type TunnelOriginCreate struct {
	Default *AssignedDefaultCreate
	Agent   *AssignedAgentCreate
	Managed *AssignedManagedCreate
}

// AssignedDefaultCreate binds the tunnel to the caller's own agent.
type AssignedDefaultCreate struct {
	LocalIP   string  `json:"local_ip"`
	LocalPort *uint16 `json:"local_port"`
}

// AssignedAgentCreate binds the tunnel to an explicit agent.
type AssignedAgentCreate struct {
	AgentID   string  `json:"agent_id"`
	LocalIP   string  `json:"local_ip"`
	LocalPort *uint16 `json:"local_port"`
}

// AssignedManagedCreate leaves routing to a managed agent.
type AssignedManagedCreate struct {
	AgentID *string `json:"agent_id"`
}

// TunnelOrigin is the read-time origin union.
//
// It has no "default" member: the server resolves a default origin into a
// concrete agent one, which is the source of the drift hazard the resource layer
// compensates for.
type TunnelOrigin struct {
	Agent   *AssignedAgent
	Managed *AssignedManaged
}

// AssignedAgent is a resolved agent origin.
type AssignedAgent struct {
	AgentID   string  `json:"agent_id"`
	AgentName string  `json:"agent_name"`
	LocalIP   string  `json:"local_ip"`
	LocalPort *uint16 `json:"local_port"`
}

// AssignedManaged is a resolved managed origin.
type AssignedManaged struct {
	AgentID   string `json:"agent_id"`
	AgentName string `json:"agent_name"`
}

// ---------------------------------------------------------------------------
// Allocation
// ---------------------------------------------------------------------------

// TunnelCreateUseAllocation is the create-time allocation union.
//
// The legacy create endpoint accepts exactly these three variants. The newer /v1
// endpoint adds "hostname" and "shared-ip", which are unreachable here and must
// not be exposed by the provider.
type TunnelCreateUseAllocation struct {
	DedicatedIP    *UseAllocDedicatedIP
	PortAllocation *UseAllocPortAlloc
	Region         *UseRegion
}

// UseAllocDedicatedIP requests placement on a dedicated IP.
type UseAllocDedicatedIP struct {
	IPHostname string  `json:"ip_hostname"`
	Port       *uint16 `json:"port"`
}

// UseAllocPortAlloc consumes an existing port allocation.
type UseAllocPortAlloc struct {
	AllocID string `json:"alloc_id"`
}

// UseRegion pins the tunnel to a region.
type UseRegion struct {
	Region PlayitNetwork `json:"region"`
}

// AccountTunnelAllocation is the read-time allocation union.
type AccountTunnelAllocation struct {
	Pending   bool
	Disabled  *TunnelDisabled
	Allocated *TunnelAllocated
}

// TunnelDisabled explains why a tunnel was refused an allocation.
type TunnelDisabled struct {
	Reason string `json:"reason"`
}

// TunnelAllocated carries the public endpoint assigned to a tunnel.
type TunnelAllocated struct {
	ID             string        `json:"id"`
	IPHostname     string        `json:"ip_hostname"`
	StaticIP4      *string       `json:"static_ip4"`
	StaticIP6      string        `json:"static_ip6"`
	AssignedDomain string        `json:"assigned_domain"`
	AssignedSRV    *string       `json:"assigned_srv"`
	TunnelIP       string        `json:"tunnel_ip"`
	PortStart      uint16        `json:"port_start"`
	PortEnd        uint16        `json:"port_end"`
	IPType         string        `json:"ip_type"`
	Region         PlayitNetwork `json:"region"`
}

// ---------------------------------------------------------------------------
// Responses
// ---------------------------------------------------------------------------

// AgentRunData is the part of /agents/rundata the provider needs.
type AgentRunData struct {
	AgentID   string `json:"agent_id"`
	AgentType string `json:"agent_type"`
}

// ObjectID is the create response: the new tunnel's identifier and nothing else.
type ObjectID struct {
	ID string `json:"id"`
}

// AllocatedPorts summarises account-wide port usage.
type AllocatedPorts struct {
	Allowed uint32 `json:"allowed"`
	Claimed uint32 `json:"claimed"`
	Desired uint32 `json:"desired"`
}

// AccountTunnels is the list response.
type AccountTunnels struct {
	Tunnels  []AccountTunnel `json:"tunnels"`
	TCPAlloc AllocatedPorts  `json:"tcp_alloc"`
	UDPAlloc AllocatedPorts  `json:"udp_alloc"`
}

// Ratelimit caps throughput on a tunnel. Nil members mean unlimited.
type Ratelimit struct {
	BytesPerSecond   *uint32 `json:"bytes_per_second"`
	PacketsPerSecond *uint32 `json:"packets_per_second"`
}

// TunnelDomain is the domain attached to a tunnel.
type TunnelDomain struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// AccountTunnel is a tunnel as the API reports it.
//
// There is no field mirroring the desired "enabled" flag: Active reflects
// observed reachability, which is false whenever the agent is offline. The two
// must not be conflated.
type AccountTunnel struct {
	ID             string                  `json:"id"`
	TunnelType     *TunnelType             `json:"tunnel_type"`
	CreatedAt      time.Time               `json:"created_at"`
	Name           *string                 `json:"name"`
	PortType       PortType                `json:"port_type"`
	PortCount      uint16                  `json:"port_count"`
	Alloc          AccountTunnelAllocation `json:"alloc"`
	Origin         *TunnelOrigin           `json:"origin"`
	Domain         *TunnelDomain           `json:"domain"`
	FirewallID     *string                 `json:"firewall_id"`
	Ratelimit      Ratelimit               `json:"ratelimit"`
	Active         bool                    `json:"active"`
	DisabledReason *string                 `json:"disabled_reason"`
	Region         *PlayitNetwork          `json:"region"`
	ProxyProtocol  *ProxyProtocol          `json:"proxy_protocol"`
}
