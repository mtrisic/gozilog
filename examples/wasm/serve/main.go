// Command serve hosts the browser demo directory over HTTP (Go's file
// server sends the correct application/wasm MIME type).
package main

import (
	"log"
	"net/http"
)

func main() {
	log.Println("serving the gozilog demo on http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", http.FileServer(http.Dir("."))))
}
