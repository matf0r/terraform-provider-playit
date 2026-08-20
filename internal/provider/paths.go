package provider

import "github.com/hashicorp/terraform-plugin-framework/path"

func pathSecretKey() path.Path     { return path.Root("secret_key") }
func pathCreateTimeout() path.Path { return path.Root("tunnel_create_timeout") }

func pathAlloc() path.Path         { return path.Root("alloc") }
func pathOriginAgentID() path.Path { return path.Root("origin").AtName("agent_id") }
func pathOriginLocalIP() path.Path { return path.Root("origin").AtName("local_ip") }
