package provider

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/matf0r/terraform-provider-playit/internal/playit"
)

// playit has no single update endpoint; six single-purpose calls cover between
// them what one PATCH would. These tests pin down which calls each change makes
// and in what order, because a redundant or missing call is invisible in a plan
// and only shows up as drift or as a tunnel briefly serving traffic under the
// wrong configuration.

func newRecordingResource(t *testing.T) (*tunnelResource, *[]string) {
	t.Helper()

	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"success","data":null}`)
	}))
	t.Cleanup(server.Close)

	return &tunnelResource{cfg: &providerConfig{
		client:        playit.NewClient("secret", playit.WithBaseURL(server.URL)),
		createTimeout: time.Minute,
	}}, &paths
}

func baseModel() tunnelModel {
	return tunnelModel{
		ID:            types.StringValue("tun-1"),
		Name:          types.StringValue("survival"),
		PortType:      types.StringValue("tcp"),
		PortCount:     types.Int64Value(1),
		Enabled:       types.BoolValue(true),
		FirewallID:    types.StringNull(),
		ProxyProtocol: types.StringNull(),
		Origin: &originModel{
			Type:      types.StringValue("agent"),
			AgentID:   types.StringValue("agent-1"),
			AgentName: types.StringValue("home"),
			LocalIP:   types.StringValue("127.0.0.1"),
			LocalPort: types.Int64Value(25565),
		},
	}
}

func TestUpdateFanOut(t *testing.T) {
	cases := []struct {
		name  string
		alter func(plan *tunnelModel)
		want  []string
	}{
		{
			name:  "no changes issue no calls",
			alter: func(*tunnelModel) {},
			want:  nil,
		},
		{
			name:  "name only",
			alter: func(p *tunnelModel) { p.Name = types.StringValue("creative") },
			want:  []string{"/tunnels/rename"},
		},
		{
			name:  "local port only",
			alter: func(p *tunnelModel) { p.Origin.LocalPort = types.Int64Value(25566) },
			want:  []string{"/tunnels/update"},
		},
		{
			name:  "enabled alone uses the dedicated endpoint",
			alter: func(p *tunnelModel) { p.Enabled = types.BoolValue(false) },
			want:  []string{"/tunnels/enable"},
		},
		{
			// /tunnels/update already carries the enabled flag, so folding the two
			// together avoids a redundant second write.
			name: "enabled with an address change is folded into update",
			alter: func(p *tunnelModel) {
				p.Origin.LocalIP = types.StringValue("10.0.0.2")
				p.Enabled = types.BoolValue(false)
			},
			want: []string{"/tunnels/update"},
		},
		{
			name:  "firewall only",
			alter: func(p *tunnelModel) { p.FirewallID = types.StringValue("fw-1") },
			want:  []string{"/tunnels/firewall/assign"},
		},
		{
			name:  "proxy protocol only",
			alter: func(p *tunnelModel) { p.ProxyProtocol = types.StringValue("proxy-protocol-v2") },
			want:  []string{"/tunnels/proxy/set"},
		},
		{
			name: "ratelimit only",
			alter: func(p *tunnelModel) {
				p.Ratelimit = &ratelimitModel{
					BytesPerSecond:   types.Int64Value(1000),
					PacketsPerSecond: types.Int64Null(),
				}
			},
			want: []string{"/tunnels/ratelimit"},
		},
		{
			// Configuration must land before the tunnel is re-enabled, so it never
			// serves traffic under a stale rate limit or PROXY setting.
			name: "everything changes in a fixed order",
			alter: func(p *tunnelModel) {
				p.Name = types.StringValue("creative")
				p.Origin.LocalIP = types.StringValue("10.0.0.2")
				p.FirewallID = types.StringValue("fw-1")
				p.ProxyProtocol = types.StringValue("proxy-protocol-v1")
				p.Ratelimit = &ratelimitModel{
					BytesPerSecond:   types.Int64Value(1000),
					PacketsPerSecond: types.Int64Value(10),
				}
				p.Enabled = types.BoolValue(false)
			},
			want: []string{
				"/tunnels/rename",
				"/tunnels/update",
				"/tunnels/firewall/assign",
				"/tunnels/proxy/set",
				"/tunnels/ratelimit",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, paths := newRecordingResource(t)

			state := baseModel()
			plan := baseModel()
			tc.alter(&plan)

			if err := r.applyChanges(context.Background(), "tun-1", &plan, &state); err != nil {
				t.Fatalf("applyChanges: %v", err)
			}
			if !reflect.DeepEqual(*paths, tc.want) {
				t.Errorf("calls\n got %v\nwant %v", *paths, tc.want)
			}
		})
	}
}

// A managed origin has no local address, so there is nothing for
// /tunnels/update to send.
func TestManagedOriginDoesNotCallUpdate(t *testing.T) {
	r, paths := newRecordingResource(t)

	state := baseModel()
	state.Origin = &originModel{
		Type:      types.StringValue("managed"),
		AgentID:   types.StringValue("agent-1"),
		AgentName: types.StringValue("home"),
		LocalIP:   types.StringNull(),
		LocalPort: types.Int64Null(),
	}
	plan := state
	planOrigin := *state.Origin
	plan.Origin = &planOrigin
	plan.Name = types.StringValue("renamed")

	if err := r.applyChanges(context.Background(), "tun-1", &plan, &state); err != nil {
		t.Fatalf("applyChanges: %v", err)
	}
	if want := []string{"/tunnels/rename"}; !reflect.DeepEqual(*paths, want) {
		t.Errorf("calls = %v, want %v", *paths, want)
	}
}

// A failing call must stop the sequence rather than push the remaining changes
// on top of a half-applied tunnel.
func TestUpdateStopsAtFirstFailure(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/tunnels/update" {
			_, _ = io.WriteString(w, `{"status":"fail","data":"TunnelNotFound"}`)
			return
		}
		_, _ = io.WriteString(w, `{"status":"success","data":null}`)
	}))
	t.Cleanup(server.Close)

	r := &tunnelResource{cfg: &providerConfig{
		client:        playit.NewClient("secret", playit.WithBaseURL(server.URL)),
		createTimeout: time.Minute,
	}}

	state := baseModel()
	plan := baseModel()
	plan.Name = types.StringValue("creative")
	plan.Origin = &originModel{
		Type:      types.StringValue("agent"),
		AgentID:   types.StringValue("agent-1"),
		AgentName: types.StringValue("home"),
		LocalIP:   types.StringValue("10.0.0.9"),
		LocalPort: types.Int64Value(25565),
	}
	plan.ProxyProtocol = types.StringValue("proxy-protocol-v1")

	err := r.applyChanges(context.Background(), "tun-1", &plan, &state)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !playit.IsFail(err, "TunnelNotFound") {
		t.Errorf("unexpected error: %v", err)
	}
	if want := []string{"/tunnels/rename", "/tunnels/update"}; !reflect.DeepEqual(paths, want) {
		t.Errorf("calls = %v, want %v (proxy/set must not run after a failure)", paths, want)
	}
}
