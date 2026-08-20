package provider

import (
	"errors"

	"github.com/hashicorp/terraform-plugin-framework/diag"

	"github.com/matf0r/terraform-provider-playit/internal/playit"
)

// failGuidance maps a playit failure code to an actionable explanation.
//
// The codes arrive as bare server-side enum names, which mean nothing to someone
// reading a terraform plan. Anything absent from this table still surfaces, just
// without the extra sentence.
var failGuidance = map[string]string{
	"AgentNotFound":                               "No agent matches origin.agent_id. The agent must be claimed on your playit account and known to the control plane.",
	"InvalidAgentId":                              "origin.agent_id is not a valid agent identifier.",
	"AgentVersionTooOld":                          "The playit agent on the host is too old for this tunnel configuration. Upgrade the agent and try again.",
	"DefaultAgentNotSupported":                    "This account cannot use the default agent origin. Set origin.agent_id explicitly.",
	"ManagedMissingAgentId":                       "A managed origin requires origin.agent_id to be set.",
	"InvalidOrigin":                               "The origin block is not a combination the API accepts. Check origin.type against the fields you set.",
	"DedicatedIpNotFound":                         "alloc.dedicated_ip.ip_hostname does not match a dedicated IP on this account.",
	"DedicatedIpPortNotAvailable":                 "The requested port is already taken on that dedicated IP.",
	"DedicatedIpNotEnoughSpace":                   "The dedicated IP has no contiguous range large enough for port_count.",
	"PortAllocNotFound":                           "alloc.port_allocation.alloc_id does not match a port allocation on this account.",
	"AllocInvalid":                                "The alloc block is not valid for this tunnel's port configuration.",
	"InvalidIpHostname":                           "alloc.dedicated_ip.ip_hostname is malformed.",
	"InvalidPortCount":                            "port_count is outside the range this account may allocate.",
	"InvalidTunnelName":                           "name contains characters the API rejects.",
	"NameTooLong":                                 "name exceeds the maximum length the API accepts.",
	"FirewallNotFound":                            "firewall_id does not match a firewall on this account.",
	"InvalidRatelimit":                            "The ratelimit values are outside the accepted range.",
	"RequiresVerifiedAccount":                     "This operation requires a verified playit account.",
	"RequiresPlayitPremium":                       "This configuration requires a playit premium subscription. Paid allocations and rate limits are premium features.",
	"PlayitPremiumRequired":                       "This configuration requires a playit premium subscription.",
	"CannotUpdateLocalAddressForUnassignedTunnel": "The tunnel has no agent assigned yet, so its local address cannot be changed.",
	"AddressOrProxyProtoNotSupportedByAgent":      "The agent does not support the requested local address or PROXY protocol version. Upgrade the agent.",
	"ChangingAgentIdNotAllowed":                   "The API refuses to move a tunnel between agents. Reaching this message means the provider failed to force replacement, which is a bug in the provider rather than a problem with your configuration.",
}

// diagnoseError converts a client error into a Terraform diagnostic.
func diagnoseError(summary string, err error) diag.Diagnostic {
	var fe *playit.FailError
	if errors.As(err, &fe) {
		detail := "The playit API rejected the request with " + fe.Code + "."
		if guidance, ok := failGuidance[fe.Code]; ok {
			detail = guidance
		}
		return diag.NewErrorDiagnostic(summary, detail)
	}

	var ae *playit.APIError
	if errors.As(err, &ae) {
		switch ae.Kind {
		case playit.KindAuth:
			return diag.NewErrorDiagnostic(summary,
				"playit rejected the credential ("+ae.Message+"). Check secret_key or "+secretKeyEnvVar+".")
		case playit.KindValidation:
			return diag.NewErrorDiagnostic(summary, "playit rejected the request as invalid: "+ae.Message)
		case playit.KindInternal:
			return diag.NewErrorDiagnostic(summary,
				"playit reported an internal error. Quote this trace when reporting it: "+ae.Message)
		}
		return diag.NewErrorDiagnostic(summary, ae.Error())
	}

	return diag.NewErrorDiagnostic(summary, err.Error())
}
