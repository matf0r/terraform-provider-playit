package playit

import (
	"encoding/json"
	"errors"
	"fmt"
)

// The payload unions in this API are serde's adjacently tagged representation,
// #[serde(tag = "type", content = "data")]:
//
//	{"type":"agent","data":{"agent_id":"..."}}
//
// Unit variants carry no content key at all:
//
//	{"type":"pending"}
//
// Each union is modelled as a struct of pointers with exactly one member set.
// Marshalling a union with zero or several members set is a programming error
// and is reported as one rather than sent to the server.

type rawUnion struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// AccountTunnelAllocation is the one union that does not use "type" as its
// discriminator: the wire form is {"status":"allocated","data":{...}}. Decoding
// it with the usual key silently yields an empty tag and fails every read.
type rawStatusUnion struct {
	Status string          `json:"status"`
	Data   json.RawMessage `json:"data"`
}

func marshalUnion(tag string, payload any) ([]byte, error) {
	if payload == nil {
		return json.Marshal(struct {
			Type string `json:"type"`
		}{Type: tag})
	}
	return json.Marshal(struct {
		Type string `json:"type"`
		Data any    `json:"data"`
	}{Type: tag, Data: payload})
}

func marshalStatusUnion(tag string, payload any) ([]byte, error) {
	if payload == nil {
		return json.Marshal(struct {
			Status string `json:"status"`
		}{Status: tag})
	}
	return json.Marshal(struct {
		Status string `json:"status"`
		Data   any    `json:"data"`
	}{Status: tag, Data: payload})
}

func unknownVariant(union, tag string) error {
	return fmt.Errorf("playit: unknown %s variant %q", union, tag)
}

// --- TunnelOriginCreate ----------------------------------------------------

func (o TunnelOriginCreate) MarshalJSON() ([]byte, error) {
	switch {
	case o.Agent != nil:
		return marshalUnion(OriginAgent, o.Agent)
	case o.Default != nil:
		return marshalUnion(OriginDefault, o.Default)
	case o.Managed != nil:
		return marshalUnion(OriginManaged, o.Managed)
	}
	return nil, errors.New("playit: TunnelOriginCreate has no variant set")
}

func (o *TunnelOriginCreate) UnmarshalJSON(b []byte) error {
	var raw rawUnion
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	*o = TunnelOriginCreate{}
	switch raw.Type {
	case OriginAgent:
		o.Agent = new(AssignedAgentCreate)
		return json.Unmarshal(raw.Data, o.Agent)
	case OriginDefault:
		o.Default = new(AssignedDefaultCreate)
		return json.Unmarshal(raw.Data, o.Default)
	case OriginManaged:
		o.Managed = new(AssignedManagedCreate)
		return json.Unmarshal(raw.Data, o.Managed)
	}
	return unknownVariant("TunnelOriginCreate", raw.Type)
}

// --- TunnelOrigin ----------------------------------------------------------

func (o TunnelOrigin) MarshalJSON() ([]byte, error) {
	switch {
	case o.Agent != nil:
		return marshalUnion(OriginAgent, o.Agent)
	case o.Managed != nil:
		return marshalUnion(OriginManaged, o.Managed)
	}
	return nil, errors.New("playit: TunnelOrigin has no variant set")
}

func (o *TunnelOrigin) UnmarshalJSON(b []byte) error {
	var raw rawUnion
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	*o = TunnelOrigin{}
	switch raw.Type {
	case OriginAgent:
		o.Agent = new(AssignedAgent)
		return json.Unmarshal(raw.Data, o.Agent)
	case OriginManaged:
		o.Managed = new(AssignedManaged)
		return json.Unmarshal(raw.Data, o.Managed)
	}
	return unknownVariant("TunnelOrigin", raw.Type)
}

// --- TunnelCreateUseAllocation ---------------------------------------------

func (a TunnelCreateUseAllocation) MarshalJSON() ([]byte, error) {
	switch {
	case a.DedicatedIP != nil:
		return marshalUnion("dedicated-ip", a.DedicatedIP)
	case a.PortAllocation != nil:
		return marshalUnion("port-allocation", a.PortAllocation)
	case a.Region != nil:
		return marshalUnion("region", a.Region)
	}
	return nil, errors.New("playit: TunnelCreateUseAllocation has no variant set")
}

func (a *TunnelCreateUseAllocation) UnmarshalJSON(b []byte) error {
	var raw rawUnion
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	*a = TunnelCreateUseAllocation{}
	switch raw.Type {
	case "dedicated-ip":
		a.DedicatedIP = new(UseAllocDedicatedIP)
		return json.Unmarshal(raw.Data, a.DedicatedIP)
	case "port-allocation":
		a.PortAllocation = new(UseAllocPortAlloc)
		return json.Unmarshal(raw.Data, a.PortAllocation)
	case "region":
		a.Region = new(UseRegion)
		return json.Unmarshal(raw.Data, a.Region)
	}
	return unknownVariant("TunnelCreateUseAllocation", raw.Type)
}

// --- AccountTunnelAllocation -----------------------------------------------

func (a AccountTunnelAllocation) MarshalJSON() ([]byte, error) {
	switch {
	case a.Allocated != nil:
		return marshalStatusUnion("allocated", a.Allocated)
	case a.Disabled != nil:
		return marshalStatusUnion("disabled", a.Disabled)
	case a.Pending:
		return marshalStatusUnion("pending", nil)
	}
	return nil, errors.New("playit: AccountTunnelAllocation has no variant set")
}

func (a *AccountTunnelAllocation) UnmarshalJSON(b []byte) error {
	var raw rawStatusUnion
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	*a = AccountTunnelAllocation{}
	switch raw.Status {
	case "pending":
		a.Pending = true
		return nil
	case "disabled":
		a.Disabled = new(TunnelDisabled)
		return json.Unmarshal(raw.Data, a.Disabled)
	case "allocated":
		a.Allocated = new(TunnelAllocated)
		return json.Unmarshal(raw.Data, a.Allocated)
	}
	return unknownVariant("AccountTunnelAllocation", raw.Status)
}
