package main

import (
	"embed"
	"log"
	"net/http"

	"veritaserum/src/dbs"
	"veritaserum/src/messaging"
	proxy "veritaserum/src/http"
	"veritaserum/src/store"
)

//go:embed ui
var uiFiles embed.FS

func main() {
	store.LoadState()

	go func() {
		log.Println("Proxy listening on :9999")
		if err := http.ListenAndServe(":9999", http.HandlerFunc(proxy.Handler)); err != nil {
			log.Fatalf("proxy: %v", err)
		}
	}()

	go dbs.StartPostgresMock("54320")
	go dbs.StartMySQLMock("33060")

	messaging.StartAPIServer("8080", uiFiles) // blocks; Gin runs the main goroutine
}
