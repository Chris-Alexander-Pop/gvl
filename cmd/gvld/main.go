package main

import (
	"log"
	"os"

	"github.com/Chris-Alexander-Pop/gvl/internal/server"
	"github.com/Chris-Alexander-Pop/gvl/internal/trace"
)

func main() {
	trace.InitFromEnv()
	opts := server.OptionsFromEnv()
	if opts.DataDir == "/data" {
		if _, err := os.Stat("/data"); err != nil {
			opts.DataDir = "./data"
		}
	}
	srv, err := server.New(opts)
	if err != nil {
		log.Fatal(err)
	}
	log.Fatal(srv.Start())
}
