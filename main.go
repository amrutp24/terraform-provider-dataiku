// terraform-provider-dataiku manages objects on a Dataiku DSS instance
// through its public REST API.
package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/amrutp24/terraform-provider-dataiku/internal/provider"
)

// These are set by GoReleaser at build time via -ldflags.
var (
	version = "dev"
	commit  = ""
)

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "run the provider with support for debuggers such as delve")
	flag.Parse()

	opts := providerserver.ServeOpts{
		// This address must match the source address users write in their
		// required_providers block.
		Address: "registry.terraform.io/amrutp24/dataiku",
		Debug:   debug,
	}

	if err := providerserver.Serve(context.Background(), provider.New(version), opts); err != nil {
		log.Fatalf("terraform-provider-dataiku (%s %s): %v", version, commit, err)
	}
}
