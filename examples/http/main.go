package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
)

func check() error {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Seen-Inject", strings.Join(r.Header.Values("X-Go-Inject"), ","))
		_, _ = io.WriteString(w, "original body")
	}))
	defer server.Close()
	client := server.Client()
	defer client.CloseIdleConnections()
	req, err := http.NewRequest(http.MethodGet, server.URL, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != 299 || resp.Status != "299 Injected" || string(body) != "original body" {
		return fmt.Errorf("response = %s %q, want 299 Injected with original body", resp.Status, body)
	}
	if got := resp.Header.Get("X-Seen-Inject"); got != "client" {
		return fmt.Errorf("request hook = %q, want client", got)
	}
	if got := resp.Header.Values("X-Go-Inject-Response"); len(got) != 1 || got[0] != "client" {
		return fmt.Errorf("response hook = %v, want one client value", got)
	}
	if req.Header.Get("X-Go-Inject") != "" {
		return fmt.Errorf("the caller's request was modified")
	}
	return nil
}

func main() {
	if err := check(); err != nil {
		panic(err)
	}
	fmt.Println("PASS http: request header, response result, original body")
}
