package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// stubAPI is an in-process stand-in for the playit control plane, faithful
// enough to drive the provider through Terraform itself.
//
// It reproduces the two behaviours that shaped the resource: allocation is
// asynchronous, so the first read of a new tunnel reports it as pending, and
// mutations are spread across single-purpose endpoints rather than one update.
type stubAPI struct {
	server *httptest.Server

	mu      sync.Mutex
	tunnels map[string]*stubTunnel
	seq     int
	calls   []string
}

type stubTunnel struct {
	id            string
	name          *string
	tunnelType    *string
	portType      string
	portCount     int
	localIP       string
	localPort     *int
	agentID       string
	managed       bool
	enabled       bool
	firewallID    *string
	proxyProtocol *string
	bytesPerSec   *int
	packetsPerSec *int
	reads         int
}

const (
	stubAgentID   = "11111111-1111-4111-8111-111111111111"
	stubAgentName = "homelab"
	stubDomain    = "demo.playit.gg"
	stubPort      = 31000
	stubSecret    = "stub-secret"
)

func newStubAPI(t *testing.T) *stubAPI {
	t.Helper()

	s := &stubAPI{tunnels: map[string]*stubTunnel{}}

	mux := http.NewServeMux()
	mux.HandleFunc("/agents/rundata", s.handleRunData)
	mux.HandleFunc("/tunnels/create", s.handleCreate)
	mux.HandleFunc("/tunnels/list", s.handleList)
	mux.HandleFunc("/tunnels/update", s.handleUpdate)
	mux.HandleFunc("/tunnels/rename", s.handleRename)
	mux.HandleFunc("/tunnels/enable", s.handleEnable)
	mux.HandleFunc("/tunnels/ratelimit", s.handleRatelimit)
	mux.HandleFunc("/tunnels/proxy/set", s.handleProxySet)
	mux.HandleFunc("/tunnels/firewall/assign", s.handleFirewallAssign)
	mux.HandleFunc("/tunnels/delete", s.handleDelete)

	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.calls = append(s.calls, r.URL.Path)
		s.mu.Unlock()
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(s.server.Close)

	return s
}

func (s *stubAPI) URL() string { return s.server.URL }

// count reports how many tunnels the stub holds, for CheckDestroy.
func (s *stubAPI) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.tunnels)
}

// deleteOutOfBand removes a tunnel behind Terraform's back, standing in for
// somebody deleting it in the playit dashboard.
func (s *stubAPI) deleteOutOfBand() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id := range s.tunnels {
		delete(s.tunnels, id)
	}
}

// --- envelope helpers ------------------------------------------------------

func stubOK(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": data})
}

// Business failures come back with HTTP 200 and a bare PascalCase code.
func stubFail(w http.ResponseWriter, code string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "fail", "data": code})
}

func stubAuthError(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": "error",
		"data":   map[string]any{"type": "auth", "message": "InvalidAgentKey"},
	})
}

func decodeBody(r *http.Request, into any) {
	_ = json.NewDecoder(r.Body).Decode(into)
}

// --- handlers --------------------------------------------------------------

func (s *stubAPI) handleRunData(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Agent-Key "+stubSecret {
		stubAuthError(w)
		return
	}
	stubOK(w, map[string]any{"agent_id": stubAgentID, "agent_type": "self-managed"})
}

func (s *stubAPI) handleCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name          *string `json:"name"`
		TunnelType    *string `json:"tunnel_type"`
		Description   *string `json:"tunnel_description"`
		PortType      string  `json:"port_type"`
		PortCount     int     `json:"port_count"`
		Enabled       bool    `json:"enabled"`
		FirewallID    *string `json:"firewall_id"`
		ProxyProtocol *string `json:"proxy_protocol"`
		Origin        struct {
			Type string `json:"type"`
			Data struct {
				AgentID   *string `json:"agent_id"`
				LocalIP   string  `json:"local_ip"`
				LocalPort *int    `json:"local_port"`
			} `json:"data"`
		} `json:"origin"`
	}
	decodeBody(r, &req)

	// A self-managed key cannot create against the account's default agent.
	if req.Origin.Type == "default" {
		stubFail(w, "InvalidAgentId")
		return
	}
	// A custom tunnel needs a description.
	if req.TunnelType == nil && (req.Description == nil || *req.Description == "") {
		stubFail(w, "TunnelTypeRequiresDescription")
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.seq++
	id := fmt.Sprintf("00000000-0000-4000-8000-%012d", s.seq)

	agentID := stubAgentID
	if req.Origin.Data.AgentID != nil && *req.Origin.Data.AgentID != "" {
		agentID = *req.Origin.Data.AgentID
	}

	s.tunnels[id] = &stubTunnel{
		id:            id,
		name:          req.Name,
		tunnelType:    req.TunnelType,
		portType:      req.PortType,
		portCount:     req.PortCount,
		localIP:       req.Origin.Data.LocalIP,
		localPort:     req.Origin.Data.LocalPort,
		agentID:       agentID,
		managed:       req.Origin.Type == "managed",
		enabled:       req.Enabled,
		firewallID:    req.FirewallID,
		proxyProtocol: req.ProxyProtocol,
	}

	stubOK(w, map[string]any{"id": id})
}

func (s *stubAPI) handleList(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TunnelID *string `json:"tunnel_id"`
	}
	decodeBody(r, &req)

	s.mu.Lock()
	defer s.mu.Unlock()

	out := []any{}
	for id, t := range s.tunnels {
		if req.TunnelID != nil && *req.TunnelID != id {
			continue
		}
		t.reads++
		out = append(out, t.render())
	}

	stubOK(w, map[string]any{
		"tunnels":   out,
		"tcp_alloc": map[string]any{"allowed": 10, "claimed": len(s.tunnels), "desired": len(s.tunnels)},
		"udp_alloc": map[string]any{"allowed": 10, "claimed": 0, "desired": 0},
	})
}

// render produces an AccountTunnel. The first read reports a pending
// allocation, so the provider's polling loop is genuinely exercised rather than
// short-circuited.
func (t *stubTunnel) render() map[string]any {
	// The real API tags this union with "status", not "type", unlike every
	// other union it returns.
	alloc := map[string]any{"status": "pending"}
	if t.reads > 1 {
		alloc = map[string]any{"status": "allocated", "data": map[string]any{
			"id":              "alloc-" + t.id,
			"ip_hostname":     "ip.example",
			"static_ip4":      nil,
			"static_ip6":      "::1",
			"assigned_domain": stubDomain,
			"assigned_srv":    nil,
			"tunnel_ip":       "203.0.113.7",
			"port_start":      stubPort,
			"port_end":        stubPort + t.portCount, // exclusive, as the real API reports it
			"assignment":      map[string]any{"type": "shared-ip"},
			"ip_type":         "both",
			"region":          "europe",
		}}
	}

	origin := map[string]any{"type": "agent", "data": map[string]any{
		"agent_id":   t.agentID,
		"agent_name": stubAgentName,
		"local_ip":   t.localIP,
		"local_port": t.localPort,
	}}
	if t.managed {
		origin = map[string]any{"type": "managed", "data": map[string]any{
			"agent_id":   t.agentID,
			"agent_name": stubAgentName,
		}}
	}

	return map[string]any{
		"id":              t.id,
		"tunnel_type":     t.tunnelType,
		"created_at":      "2026-08-20T00:00:00Z",
		"name":            t.name,
		"port_type":       t.portType,
		"port_count":      t.portCount,
		"alloc":           alloc,
		"origin":          origin,
		"domain":          nil,
		"firewall_id":     t.firewallID,
		"ratelimit":       map[string]any{"bytes_per_second": t.bytesPerSec, "packets_per_second": t.packetsPerSec},
		"active":          true,
		"disabled_reason": nil,
		"region":          "europe",
		"proxy_protocol":  t.proxyProtocol,
	}
}

func (s *stubAPI) withTunnel(w http.ResponseWriter, id string, fn func(*stubTunnel)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, found := s.tunnels[id]
	if !found {
		stubFail(w, "TunnelNotFound")
		return
	}
	fn(t)
	stubOK(w, nil)
}

func (s *stubAPI) handleUpdate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TunnelID  string `json:"tunnel_id"`
		LocalIP   string `json:"local_ip"`
		LocalPort *int   `json:"local_port"`
		Enabled   bool   `json:"enabled"`
	}
	decodeBody(r, &req)
	s.withTunnel(w, req.TunnelID, func(t *stubTunnel) {
		t.localIP, t.localPort, t.enabled = req.LocalIP, req.LocalPort, req.Enabled
	})
}

func (s *stubAPI) handleRename(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TunnelID string `json:"tunnel_id"`
		Name     string `json:"name"`
	}
	decodeBody(r, &req)
	s.withTunnel(w, req.TunnelID, func(t *stubTunnel) { t.name = &req.Name })
}

func (s *stubAPI) handleEnable(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TunnelID string `json:"tunnel_id"`
		Enabled  bool   `json:"enabled"`
	}
	decodeBody(r, &req)
	s.withTunnel(w, req.TunnelID, func(t *stubTunnel) { t.enabled = req.Enabled })
}

func (s *stubAPI) handleRatelimit(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TunnelID         string `json:"tunnel_id"`
		BytesPerSecond   *int   `json:"bytes_per_second"`
		PacketsPerSecond *int   `json:"packets_per_second"`
	}
	decodeBody(r, &req)
	s.withTunnel(w, req.TunnelID, func(t *stubTunnel) {
		t.bytesPerSec, t.packetsPerSec = req.BytesPerSecond, req.PacketsPerSecond
	})
}

func (s *stubAPI) handleProxySet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TunnelID      string  `json:"tunnel_id"`
		ProxyProtocol *string `json:"proxy_protocol"`
	}
	decodeBody(r, &req)
	s.withTunnel(w, req.TunnelID, func(t *stubTunnel) { t.proxyProtocol = req.ProxyProtocol })
}

func (s *stubAPI) handleFirewallAssign(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TunnelID   string  `json:"tunnel_id"`
		FirewallID *string `json:"firewall_id"`
	}
	decodeBody(r, &req)
	s.withTunnel(w, req.TunnelID, func(t *stubTunnel) { t.firewallID = req.FirewallID })
}

func (s *stubAPI) handleDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TunnelID string `json:"tunnel_id"`
	}
	decodeBody(r, &req)

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, found := s.tunnels[req.TunnelID]; !found {
		stubFail(w, "TunnelNotFound")
		return
	}
	delete(s.tunnels, req.TunnelID)
	stubOK(w, nil)
}
