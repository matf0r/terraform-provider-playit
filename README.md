# terraform-provider-playit

[![Test](https://github.com/matf0r/terraform-provider-playit/actions/workflows/test.yml/badge.svg)](https://github.com/matf0r/terraform-provider-playit/actions/workflows/test.yml)
[![Go](https://img.shields.io/github/go-mod/go-version/matf0r/terraform-provider-playit?logo=go&logoColor=white)](go.mod)
[![License](https://img.shields.io/badge/license-MPL--2.0-blue)](LICENSE)

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
      source = "matf0r/playit"
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

The provider authenticates with a **playit agent secret key** — a hex string that authorises every
control plane action: listing, creating, updating and destroying tunnels.

Supply it through the `PLAYIT_SECRET_KEY` environment variable, or the `secret_key` provider
attribute. Prefer the environment variable: the attribute is marked sensitive and is kept out of
logs, but a value written into a `.tf` file still ends up in version control.

### On a host that already runs the agent

`playit secret-path` prints the path of the file holding the key, and `playit status` reports the
same path along with whether a secret is currently loaded:

```sh
playit secret-path
```

The agent resolves that path in order:

| Order | Location |
| --- | --- |
| 1 | `./playit.toml` in the working directory |
| 2 | `/etc/playit/playit.toml` — Linux, when the service is installed |
| 3 | `playit.toml` in the platform configuration directory |

The file comes in one of two shapes: the bare key as the entire contents, or TOML carrying a
`secret_key` entry. Reading it with `cat` therefore does not always give you the key. This handles
both, since the key is the only long hex run in either form:

```sh
export PLAYIT_SECRET_KEY="$(grep -oE '[0-9a-fA-F]{16,}' "$(playit secret-path)" | head -1)"
```

Reading `/etc/playit/playit.toml` usually needs root.

### When the agent runs in Docker

The official image takes the key as an environment variable rather than a file:

```yaml
services:
  playit:
    image: ghcr.io/playit-cloud/playit-agent:0.15
    environment:
      - SECRET_KEY=${PLAYIT_SECRET}
```

There is usually no volume and no `playit.toml` inside the container, so `playit secret-path` does
not apply — there is no file for it to point at. Read the value back from the container instead:

```sh
docker inspect <container> --format '{{range .Config.Env}}{{println .}}{{end}}' \
  | sed -n 's/^SECRET_KEY=//p'
```

That reports what the running container was started with, which is not necessarily what the compose
file says today: editing the `.env` changes nothing until the container is recreated. Where the two
disagree, the container is what is actually authenticating, and the `.env` is what the next
`docker compose up -d` will use.

Note that container environment variables are readable by anyone who can talk to the Docker daemon,
so a key supplied this way is only as private as access to the host.

### Without an agent installed

Claim a new agent from your playit account. The Docker setup page,
[playit.gg/account/agents/new-docker](https://playit.gg/account/agents/new-docker), shows the
`SECRET_KEY` as part of the command it generates; the key is valid whether or not you go on to run
the agent under Docker.

Keys cannot be created through the API — claiming an agent is a browser flow — which is why the
provider treats agents as out of scope.

### For the acceptance tests in CI

The acceptance job reads the key from a repository secret of the same name:

```sh
gh secret set PLAYIT_SECRET_KEY --repo matf0r/terraform-provider-playit
```

Those tests run against the real playit API and create tunnels on the account the key belongs to, so
point it at an account you are willing to have written to. The rest of the suite drives the provider
against an in-process stub and needs no secret at all.

## Requirements

- [Terraform](https://developer.hashicorp.com/terraform/downloads) >= 1.0
- [Go](https://go.dev/dl/) 1.25+ (to build from source; the pinned version is in `.go-version`)
- A playit.gg account with a claimed agent

## Development

```sh
go build ./...
go test ./...
```

Part of the suite drives the provider through Terraform itself against an in-process stub of the
playit API, so a `terraform` binary must be on `PATH`. Those tests need no credentials and make no
network calls.

To try the provider before it is published, point Terraform at your local build with a
`dev_overrides` block in `~/.terraformrc`:

```hcl
provider_installation {
  dev_overrides {
    "matf0r/playit" = "/path/to/your/gopath/bin"
  }
  direct {}
}
```

Documentation under `docs/` is generated from the schema and from the examples, and CI fails if the
committed output is stale:

```sh
go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs@latest generate
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
