package main

import (
	"flag"
	"log"
	"time"

	"github.com/mpolden/atbapi/atb"
	"github.com/mpolden/atbapi/http"
)

func mustParseDuration(s string) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		log.Fatal(err)
	}
	return d
}

func main() {
	listen := flag.String("l", ":8080", "Listen address")
	config := flag.String("c", "config.json", "Path to config file")
	stopTTL := flag.String("s", "168h", "Bus stop cache duration")
	departureTTL := flag.String("d", "1m", "Departure cache duration")
	cors := flag.Bool("x", false, "Allow requests from other domains")
	flag.Parse()

	client, err := atb.NewFromConfig(*config)
	if err != nil {
		log.Fatal(err)
	}

	server := http.New(client, mustParseDuration(*stopTTL), mustParseDuration(*departureTTL), *cors)

	log.Printf("Listening on %s", *listen)
	if err := server.ListenAndServe(*listen); err != nil {
		log.Fatal(err)
	}
}