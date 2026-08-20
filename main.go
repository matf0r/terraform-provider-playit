// terraform-provider-playit is the Terraform provider for playit.gg.
package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/matf0r/terraform-provider-playit/internal/provider"
)

// version is overwritten at build time by GoReleaser.
var version = "dev"

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "run the provider with support for debuggers such as delve")
	flag.Parse()

	err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{
		Address: "registry.terraform.io/matf0r/playit",
		Debug:   debug,
	})
	if err != nil {
		log.Fatal(err)
	}
}
