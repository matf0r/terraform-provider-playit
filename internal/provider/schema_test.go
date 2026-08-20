package provider

import (
	"context"
	"testing"

	fwprovider "github.com/hashicorp/terraform-plugin-framework/provider"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
)

// ValidateImplementation catches schema mistakes that would otherwise only
// surface when Terraform actually loads the provider: attributes that are
// neither optional, required nor computed, defaults on non-computed attributes,
// plan modifiers on the wrong kind of attribute, and so on.

func TestTunnelResourceSchemaIsValid(t *testing.T) {
	ctx := context.Background()

	resp := &fwresource.SchemaResponse{}
	NewTunnelResource().Schema(ctx, fwresource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("building the schema produced diagnostics: %+v", resp.Diagnostics)
	}

	if diags := resp.Schema.ValidateImplementation(ctx); diags.HasError() {
		t.Fatalf("schema is not implementable: %+v", diags)
	}
}

func TestProviderSchemaIsValid(t *testing.T) {
	ctx := context.Background()

	resp := &fwprovider.SchemaResponse{}
	New("test")().Schema(ctx, fwprovider.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("building the schema produced diagnostics: %+v", resp.Diagnostics)
	}

	if diags := resp.Schema.ValidateImplementation(ctx); diags.HasError() {
		t.Fatalf("schema is not implementable: %+v", diags)
	}
}
