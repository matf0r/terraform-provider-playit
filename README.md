# terraform-provider-playit

A [Terraform](https://www.terraform.io) provider for [playit.gg](https://playit.gg), built on the
[Terraform Plugin Framework](https://github.com/hashicorp/terraform-plugin-framework).

playit.gg exposes self-hosted services — game servers in particular — to the public internet without
port forwarding or firewall changes. This provider manages the playit **control plane** from
Terraform, so tunnels can live in the same configuration as the infrastructure they front.

> **Status: pre-release, under active development.** Nothing is published to the Terraform Registry
> yet and the schema may still change without notice.

## Scope

The provider manages the control plane only. It does **not** install, configure, or supervise the
playit agent — the agent is expected to be running on the host already.

| Object | Support |
|---|---|
| Tunnels | Full CRUD |
| Domains | Read-only — the API exposes no create/bind/delete |
| Agents | Read-only — agents are created through playit's browser claim flow, not the API |
| Port allocations | Not a standalone object; configured as part of a tunnel |

## Usage

```hcl
terraform {
  required_providers {
    playit = {
      source = "vshxp/playit"
    }
  }
}

provider "playit" {
  # or set PLAYIT_SECRET_KEY
  secret_key = var.playit_secret_key
}

resource "playit_tunnel" "minecraft" {
  name        = "survival-server"
  tunnel_type = "minecraft-java"
  port_type   = "tcp"

  origin = {
    local_ip   = "127.0.0.1"
    local_port = 25565
  }
}

output "server_address" {
  value = playit_tunnel.minecraft.public_address
}
```

## Authentication

The provider authenticates with a playit agent secret key, taken from the `secret_key` provider
attribute or the `PLAYIT_SECRET_KEY` environment variable.

The key is the one your agent already uses. Find it with `playit secret-path`, or in the agent's
`playit.toml`.

```sh
export PLAYIT_SECRET_KEY="$(cat "$(playit secret-path)")"
```

## Requirements

- [Terraform](https://developer.hashicorp.com/terraform/downloads) >= 1.0
- [Go](https://go.dev/dl/) 1.25+ (to build from source; the pinned version is in `.go-version`)
- A playit.gg account with a claimed agent

## Development

```sh
go build ./...
go test ./...
```

To try the provider before it is published, point Terraform at your local build with a
`dev_overrides` block in `~/.terraformrc`:

```hcl
provider_installation {
  dev_overrides {
    "vshxp/playit" = "/path/to/your/gopath/bin"
  }
  direct {}
}
```

Documentation is generated from the schema. The provider name has to be passed explicitly, because
`tfplugindocs` otherwise infers it from the directory name:

```sh
go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs@latest generate --provider-name playit
```

Acceptance tests talk to the real playit API and are skipped unless explicitly enabled:

```sh
TF_ACC=1 PLAYIT_SECRET_KEY=... go test ./... -v
```

## Documentation

Reference documentation for the provider and its resources lives in [`docs/`](docs/),
generated from the schema with `tfplugindocs`.

## License

Mozilla Public License 2.0.
