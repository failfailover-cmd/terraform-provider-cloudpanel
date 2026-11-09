package main

import (
	"context"
	"flag"
	"log"

	"github.com/failfailover-cmd/terraform-provider-cloudpanel/internal/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
)

var version = "dev"

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "set true for delve")
	flag.Parse()

	opts := providerserver.ServeOpts{
		Address: "registry.terraform.io/failfailover-cmd/cloudpanel",
		Debug:   debug,
	}

	if err := providerserver.Serve(context.Background(), provider.New(version), opts); err != nil {
		log.Fatal(err)
	}
}
