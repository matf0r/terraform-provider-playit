package provider

import (
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/vshxp/terraform-provider-playit/internal/playit"
)

// The playit API models optional fields as Option<T> and expects an explicit
// null rather than an absent key, so every conversion below produces a pointer
// instead of relying on omitempty.

func stringPtr(v types.String) *string {
	if v.IsNull() || v.IsUnknown() || v.ValueString() == "" {
		return nil
	}
	s := v.ValueString()
	return &s
}

func proxyProtocolPtr(v types.String) *playit.ProxyProtocol {
	if v.IsNull() || v.IsUnknown() || v.ValueString() == "" {
		return nil
	}
	p := playit.ProxyProtocol(v.ValueString())
	return &p
}

func uint16Ptr(v types.Int64) *uint16 {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	n := uint16(v.ValueInt64())
	return &n
}

func uint32Ptr(v types.Int64) *uint32 {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	n := uint32(v.ValueInt64())
	return &n
}

func uint16Value(v types.Int64, fallback uint16) uint16 {
	if v.IsNull() || v.IsUnknown() {
		return fallback
	}
	return uint16(v.ValueInt64())
}

func boolValue(v types.Bool, fallback bool) bool {
	if v.IsNull() || v.IsUnknown() {
		return fallback
	}
	return v.ValueBool()
}

func stringOrNull(v *string) types.String {
	if v == nil {
		return types.StringNull()
	}
	return types.StringValue(*v)
}

func int64OrNull16(v *uint16) types.Int64 {
	if v == nil {
		return types.Int64Null()
	}
	return types.Int64Value(int64(*v))
}

func int64OrNull32(v *uint32) types.Int64 {
	if v == nil {
		return types.Int64Null()
	}
	return types.Int64Value(int64(*v))
}

func itoa(v int64) string { return strconv.FormatInt(v, 10) }
