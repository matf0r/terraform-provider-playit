terraform {
  required_providers {
    playit = {
      source = "matf0r/playit"
    }
  }
}

# The key is the one your playit agent already uses. Rather than writing it into
# the configuration, export it:
#
#   export PLAYIT_SECRET_KEY="$(cat "$(playit secret-path)")"
provider "playit" {}
