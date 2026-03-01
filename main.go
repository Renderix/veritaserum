package main

import (
	"embed"
	"flag"
	"log"
	"net/http"
	"time"

	"veritaserum/src/dbs"
	proxy "veritaserum/src/http"
	"veritaserum/src/messaging"
	"veritaserum/src/store"
)

//go:embed dist
var distFiles embed.FS

func main() {
	replay  := flag.Bool("replay", false, "headless replay mode — loads suite JSON, no UI")
	suite   := flag.String("suite", "", "path to suite JSON file (required with --replay)")
	timeout := flag.Duration("timeout", 0, "auto-exit after duration, e.g. 120s (replay mode only)")
	flag.Parse()

	if *replay {
		if *suite == "" {
			log.Fatal("--suite is required in --replay mode")
		}
		if err := store.LoadSuite(*suite); err != nil {
			log.Fatalf("load suite: %v", err)
		}
		log.Printf("Replay mode: loaded suite %s", *suite)
	} else {
		store.LoadState()
	}

	go func() {
		log.Println("Proxy      listening on :9999")
		if err := http.ListenAndServe(":9999", http.HandlerFunc(proxy.Handler)); err != nil {
			log.Fatalf("proxy: %v", err)
		}
	}()

	go dbs.StartPostgresMock("54320")
	go dbs.StartMySQLMock("33060")
	go dbs.StartRedisMock("6380")

	if *replay && *timeout > 0 {
		go func() {
			time.Sleep(*timeout)
			log.Fatalf("replay timeout (%s) reached, exiting", *timeout)
		}()
	}

	messaging.StartAPIServer("8080", distFiles) // blocks
}
