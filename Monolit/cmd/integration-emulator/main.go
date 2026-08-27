package main

import (
	"log"
	"net/http"
	"os"

	"verbatrace/monolit/internal/integrationemulator"
)

func main() {
	address := os.Getenv("INTEGRATION_EMULATOR_ADDRESS")
	if address == "" {
		address = "127.0.0.1:8091"
	}
	log.Printf("integration emulator listening on %s", address)
	if err := http.ListenAndServe(address, integrationemulator.New().Handler()); err != nil {
		log.Fatal(err)
	}
}
