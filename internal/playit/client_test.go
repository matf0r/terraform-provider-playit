package playit

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// stub records what the client sent and replies with a canned body.
type stub struct {
	server   *httptest.Server
	paths    []string
	bodies   []string
	authSeen []string
}

func newStub(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) *stub {
	t.Helper()
	s := &stub{}
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s.paths = append(s.paths, r.URL.Path)
		s.bodies = append(s.bodies, string(body))
		s.authSeen = append(s.authSeen, r.Header.Get("Authorization"))
		handler(w, r)
	}))
	t.Cleanup(s.server.Close)
	return s
}

func (s *stub) client() *Client {
	return NewClient("secret-123", WithBaseURL(s.server.URL))
}

func respond(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

func TestSuccessEnvelopeIsUnwrapped(t *testing.T) {
	s := newStub(t, func(w http.ResponseWriter, _ *http.Request) {
		respond(w, http.StatusOK, `{"status":"success","data":{"id":"tun-1"}}`)
	})

	got, err := s.client().TunnelsCreate(context.Background(), minimalCreate())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != "tun-1" {
		t.Errorf("id = %q, want %q", got.ID, "tun-1")
	}
	if s.paths[0] != "/tunnels/create" {
		t.Errorf("path = %q", s.paths[0])
	}
}

// The API answers HTTP 200 for business failures. Treating the status code as
// the outcome would read every one of them as a success.
func TestFailEnvelopeOnHTTP200BecomesFailError(t *testing.T) {
	s := newStub(t, func(w http.ResponseWriter, _ *http.Request) {
		respond(w, http.StatusOK, `{"status":"fail","data":"TunnelNotFound"}`)
	})

	err := s.client().TunnelsDelete(context.Background(), ReqTunnelsDelete{TunnelID: "tun-1"})
	if err == nil {
		t.Fatal("expected an error for a fail envelope returned with HTTP 200")
	}
	if !IsFail(err, "TunnelNotFound") {
		t.Errorf("IsFail(TunnelNotFound) = false, err = %v", err)
	}
	if IsFail(err, "SomethingElse") {
		t.Error("IsFail matched the wrong code")
	}
}

func TestErrorEnvelopeUsesMessageKeyNotData(t *testing.T) {
	s := newStub(t, func(w http.ResponseWriter, _ *http.Request) {
		respond(w, http.StatusOK, `{"status":"error","data":{"type":"auth","message":"InvalidAgentKey"}}`)
	})

	_, err := s.client().AgentsRunData(context.Background())
	if err == nil {
		t.Fatal("expected an error")
	}
	if !IsAuth(err) {
		t.Fatalf("IsAuth = false, err = %v", err)
	}
	var apiErr *APIError
	if !asAPIError(err, &apiErr) {
		t.Fatalf("not an APIError: %v", err)
	}
	if apiErr.Message != "InvalidAgentKey" {
		t.Errorf("message = %q, want %q", apiErr.Message, "InvalidAgentKey")
	}
}

// path-not-found and internal carry an object where auth carries a string.
func TestErrorEnvelopeWithObjectMessage(t *testing.T) {
	s := newStub(t, func(w http.ResponseWriter, _ *http.Request) {
		respond(w, http.StatusOK, `{"status":"error","data":{"type":"internal","message":{"trace_id":"abc"}}}`)
	})

	_, err := s.client().AgentsRunData(context.Background())
	var apiErr *APIError
	if !asAPIError(err, &apiErr) {
		t.Fatalf("not an APIError: %v", err)
	}
	if apiErr.Kind != KindInternal {
		t.Errorf("kind = %q", apiErr.Kind)
	}
	if apiErr.Message == "" {
		t.Error("object-shaped message was dropped")
	}
}

func TestAuthorizationUsesAgentKeyScheme(t *testing.T) {
	s := newStub(t, func(w http.ResponseWriter, _ *http.Request) {
		respond(w, http.StatusOK, `{"status":"success","data":null}`)
	})

	_ = s.client().TunnelsDelete(context.Background(), ReqTunnelsDelete{TunnelID: "tun-1"})

	if want := "Agent-Key secret-123"; s.authSeen[0] != want {
		t.Errorf("Authorization = %q, want %q", s.authSeen[0], want)
	}
}

func TestVoidEndpointAcceptsNullData(t *testing.T) {
	s := newStub(t, func(w http.ResponseWriter, _ *http.Request) {
		respond(w, http.StatusOK, `{"status":"success","data":null}`)
	})

	if err := s.client().TunnelsRename(context.Background(), ReqTunnelsRename{TunnelID: "t", Name: "n"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRateLimitIsRetried(t *testing.T) {
	var calls int32
	s := newStub(t, func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			respond(w, http.StatusTooManyRequests, `{}`)
			return
		}
		respond(w, http.StatusOK, `{"status":"success","data":{"id":"tun-9"}}`)
	})

	got, err := s.client().TunnelsCreate(context.Background(), minimalCreate())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != "tun-9" {
		t.Errorf("id = %q", got.ID)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2 (one rate-limited, one successful)", calls)
	}
}

// A business failure is an outcome, not a fault; retrying it would multiply
// side effects for no benefit.
func TestFailIsNotRetried(t *testing.T) {
	var calls int32
	s := newStub(t, func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		respond(w, http.StatusOK, `{"status":"fail","data":"InvalidPortCount"}`)
	})

	_, _ = s.client().TunnelsCreate(context.Background(), minimalCreate())
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
}

// Reads are POSTs with a server-side filter, not a full account listing.
func TestTunnelsGetFiltersServerSide(t *testing.T) {
	s := newStub(t, func(w http.ResponseWriter, _ *http.Request) {
		respond(w, http.StatusOK, `{"status":"success","data":{"tunnels":[
			{"id":"tun-1","port_type":"tcp","port_count":1,"created_at":"2026-01-01T00:00:00Z",
			 "alloc":{"status":"pending"},"ratelimit":{"bytes_per_second":null,"packets_per_second":null},
			 "active":true}
		],"tcp_alloc":{"allowed":1,"claimed":1,"desired":1},"udp_alloc":{"allowed":0,"claimed":0,"desired":0}}}`)
	})

	got, err := s.client().TunnelsGet(context.Background(), "tun-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("tunnel not found")
	}

	var sent ReqTunnelsList
	if err := json.Unmarshal([]byte(s.bodies[0]), &sent); err != nil {
		t.Fatalf("request body did not decode: %v", err)
	}
	if sent.TunnelID == nil || *sent.TunnelID != "tun-1" {
		t.Errorf("tunnel_id filter not sent: %s", s.bodies[0])
	}
}

func TestTunnelsGetReturnsNilWhenAbsent(t *testing.T) {
	s := newStub(t, func(w http.ResponseWriter, _ *http.Request) {
		respond(w, http.StatusOK, `{"status":"success","data":{"tunnels":[],
			"tcp_alloc":{"allowed":0,"claimed":0,"desired":0},
			"udp_alloc":{"allowed":0,"claimed":0,"desired":0}}}`)
	})

	got, err := s.client().TunnelsGet(context.Background(), "missing")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("got %+v, want nil", got)
	}
}

// Optional fields must serialise as explicit nulls: the server models them as
// Option<T> and rejects payloads with the keys missing.
func TestOptionalFieldsSerialiseAsNull(t *testing.T) {
	s := newStub(t, func(w http.ResponseWriter, _ *http.Request) {
		respond(w, http.StatusOK, `{"status":"success","data":{"id":"x"}}`)
	})

	_, _ = s.client().TunnelsCreate(context.Background(), ReqTunnelsCreate{
		PortType:  PortTypeTCP,
		PortCount: 1,
		Origin:    TunnelOriginCreate{Default: &AssignedDefaultCreate{LocalIP: "127.0.0.1"}},
	})

	var payload map[string]json.RawMessage
	if err := json.Unmarshal([]byte(s.bodies[0]), &payload); err != nil {
		t.Fatalf("body did not decode: %v", err)
	}
	for _, key := range []string{"name", "tunnel_type", "alloc", "firewall_id", "proxy_protocol"} {
		raw, ok := payload[key]
		if !ok {
			t.Errorf("%q was omitted; the API expects an explicit null", key)
			continue
		}
		if string(raw) != "null" {
			t.Errorf("%q = %s, want null", key, raw)
		}
	}
}

// minimalCreate is the smallest create request that marshals: the origin union
// refuses to serialise with no variant set, which is the guard working.
func minimalCreate() ReqTunnelsCreate {
	return ReqTunnelsCreate{
		PortType:  PortTypeTCP,
		PortCount: 1,
		Origin:    TunnelOriginCreate{Default: &AssignedDefaultCreate{LocalIP: "127.0.0.1"}},
	}
}

func asAPIError(err error, target **APIError) bool {
	e, ok := err.(*APIError)
	if ok {
		*target = e
	}
	return ok
}
