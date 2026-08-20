package provider

import (
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// Shorthands that keep the schema readable; the framework's own names are long
// enough that the attribute table becomes hard to scan without them.
type (
	validatorString = validator.String
	validatorInt64  = validator.Int64
)

func computedString(description string) schema.StringAttribute {
	return schema.StringAttribute{MarkdownDescription: description, Computed: true}
}

func computedBool(description string) schema.BoolAttribute {
	return schema.BoolAttribute{MarkdownDescription: description, Computed: true}
}

func computedInt64(description string) schema.Int64Attribute {
	return schema.Int64Attribute{MarkdownDescription: description, Computed: true}
}

// joinBackticked renders an enum for documentation as `a`, `b`, `c`.
func joinBackticked(values []string) string {
	return strings.Join(values, "`, `")
}
