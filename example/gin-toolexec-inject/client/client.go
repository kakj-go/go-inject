package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
)

func main() {
	url := flag.String("server", "http://localhost:8080/hello", "server url")
	resp, err := http.Post(*url, "application/json", bytes.NewBuffer([]byte("aaa")))
	if err != nil {
		log.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatal(err)
	}
	_ = resp.Body.Close()
	fmt.Printf("Response Received: %s\n", body)
}
