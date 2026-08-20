# A Minecraft server on the machine running the playit agent.
resource "playit_tunnel" "minecraft" {
  name        = "survival-server"
  tunnel_type = "minecraft-java"
  port_type   = "tcp"

  origin = {
    local_ip   = "127.0.0.1"
    local_port = 25565
  }
}

# Hand the address to players. For minecraft-java this is an SRV record, so it
# carries no port.
output "server_address" {
  value = playit_tunnel.minecraft.public_address
}

# A custom tunnel: omit tunnel_type entirely. There is no "custom" value.
resource "playit_tunnel" "syncthing" {
  name      = "syncthing"
  port_type = "both"

  origin = {
    local_ip   = "192.168.1.20"
    local_port = 22000
  }
}

# Pinning a tunnel to a specific agent, rather than this account's default one.
resource "playit_tunnel" "valheim" {
  name        = "valheim"
  tunnel_type = "valheim"
  port_type   = "udp"
  port_count  = 3

  origin = {
    agent_id   = "11111111-1111-4111-8111-111111111111"
    local_ip   = "10.0.0.5"
    local_port = 2456
  }
}
