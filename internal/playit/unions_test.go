package playit

import (
	"encoding/json"
	"reflect"
	"testing"
)

// This API uses four different serialisation conventions at once: the envelope
// is tagged status/data, payload unions are tagged type/data, value enums are
// kebab-case strings, and error enums are bare PascalCase. Conflating any two of
// them produces payloads the server silently rejects, so every variant is
// round-tripped here.

func TestUnionWireFormat(t *testing.T) {
	port := uint16(25565)

	cases := []struct {
		name  string
		value any
		wire  string
	}{
		{
			name:  "origin create default",
			value: TunnelOriginCreate{Default: &AssignedDefaultCreate{LocalIP: "127.0.0.1", LocalPort: &port}},
			wire:  `{"type":"default","data":{"local_ip":"127.0.0.1","local_port":25565}}`,
		},
		{
			name:  "origin create agent",
			value: TunnelOriginCreate{Agent: &AssignedAgentCreate{AgentID: "a-1", LocalIP: "10.0.0.5", LocalPort: nil}},
			wire:  `{"type":"agent","data":{"agent_id":"a-1","local_ip":"10.0.0.5","local_port":null}}`,
		},
		{
			name:  "origin create managed",
			value: TunnelOriginCreate{Managed: &AssignedManagedCreate{AgentID: nil}},
			wire:  `{"type":"managed","data":{"agent_id":null}}`,
		},
		{
			name:  "alloc dedicated ip",
			value: TunnelCreateUseAllocation{DedicatedIP: &UseAllocDedicatedIP{IPHostname: "ip.example", Port: &port}},
			wire:  `{"type":"dedicated-ip","data":{"ip_hostname":"ip.example","port":25565}}`,
		},
		{
			name:  "alloc port allocation",
			value: TunnelCreateUseAllocation{PortAllocation: &UseAllocPortAlloc{AllocID: "alloc-1"}},
			wire:  `{"type":"port-allocation","data":{"alloc_id":"alloc-1"}}`,
		},
		{
			name:  "alloc region",
			value: TunnelCreateUseAllocation{Region: &UseRegion{Region: "north-america"}},
			wire:  `{"type":"region","data":{"region":"north-america"}}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := json.Marshal(tc.value)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(got) != tc.wire {
				t.Errorf("marshal\n got %s\nwant %s", got, tc.wire)
			}
		})
	}
}

// Unit variants carry no data key at all.
//
// Note the discriminator: allocation is the one union the API tags with
// "status" rather than "type".
func TestPendingAllocationHasNoDataKey(t *testing.T) {
	got, err := json.Marshal(AccountTunnelAllocation{Pending: true})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if want := `{"status":"pending"}`; string(got) != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

// Decoding allocation with the usual "type" key yields an empty tag and fails
// every read of every tunnel, which is exactly what shipped once. This pins the
// shape a live account actually returns.
func TestAllocationUsesStatusAsDiscriminator(t *testing.T) {
	const wire = `{"status":"allocated","data":{
		"id":"77fe94d2-a61a-43ad-bbfb-88850f000000","ip_hostname":"ip.example",
		"static_ip4":null,"static_ip6":"::1","assigned_domain":"cobra.playit.plus",
		"assigned_srv":null,"tunnel_ip":"203.0.113.7","port_start":31000,"port_end":31000,
		"assignment":{"type":"shared-ip"},"ip_type":"both","region":"south-america"}}`

	var alloc AccountTunnelAllocation
	if err := json.Unmarshal([]byte(wire), &alloc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if alloc.Allocated == nil {
		t.Fatal("allocated variant not decoded")
	}
	if got := alloc.Allocated.AssignedDomain; got != "cobra.playit.plus" {
		t.Errorf("assigned_domain = %q", got)
	}
}

// Every other union really does use "type"; the origin of a live tunnel proves
// the two conventions coexist.
func TestOriginStillUsesTypeAsDiscriminator(t *testing.T) {
	const wire = `{"type":"agent","data":{"agent_id":"2330714f-10e7-48f0-b31a-f56d73ce1af5",
		"agent_name":"homelab","local_ip":"192.168.3.20","local_port":27015}}`

	var origin TunnelOrigin
	if err := json.Unmarshal([]byte(wire), &origin); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if origin.Agent == nil {
		t.Fatal("agent variant not decoded")
	}
}

func TestUnionRoundTrip(t *testing.T) {
	port := uint16(7777)

	t.Run("origin create", func(t *testing.T) {
		for _, original := range []TunnelOriginCreate{
			{Default: &AssignedDefaultCreate{LocalIP: "127.0.0.1", LocalPort: &port}},
			{Agent: &AssignedAgentCreate{AgentID: "a-1", LocalIP: "127.0.0.1", LocalPort: &port}},
			{Managed: &AssignedManagedCreate{}},
		} {
			b, err := json.Marshal(original)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var back TunnelOriginCreate
			if err := json.Unmarshal(b, &back); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if !reflect.DeepEqual(original, back) {
				t.Errorf("round trip changed the value:\n got %+v\nwant %+v", back, original)
			}
		}
	})

	t.Run("account allocation", func(t *testing.T) {
		for _, original := range []AccountTunnelAllocation{
			{Pending: true},
			{Disabled: &TunnelDisabled{Reason: "requires-premium"}},
			{Allocated: &TunnelAllocated{
				ID: "alloc-1", IPHostname: "ip.example", AssignedDomain: "x.playit.gg",
				TunnelIP: "1.2.3.4", PortStart: 1000, PortEnd: 1001, Region: "europe",
			}},
		} {
			b, err := json.Marshal(original)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var back AccountTunnelAllocation
			if err := json.Unmarshal(b, &back); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if !reflect.DeepEqual(original, back) {
				t.Errorf("round trip changed the value:\n got %+v\nwant %+v", back, original)
			}
		}
	})
}

// The read-side origin union has no "default" member: the server resolves a
// default origin into a concrete agent one.
func TestReadOriginRejectsDefault(t *testing.T) {
	var o TunnelOrigin
	err := json.Unmarshal([]byte(`{"type":"default","data":{}}`), &o)
	if err == nil {
		t.Fatal("expected an error decoding a default origin on the read union")
	}
}

func TestUnsetUnionIsRefusedLocally(t *testing.T) {
	if _, err := json.Marshal(TunnelOriginCreate{}); err == nil {
		t.Error("marshalling an empty union should fail rather than send a bad payload")
	}
	if _, err := json.Marshal(TunnelCreateUseAllocation{}); err == nil {
		t.Error("marshalling an empty allocation union should fail")
	}
}
