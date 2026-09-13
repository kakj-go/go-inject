package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/gin-gonic/gin"
)

func check() error {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.GET("/hello/:name", func(c *gin.Context) {
		c.Header("X-Seen-Inject", strings.Join(c.Request.Header.Values("X-Go-Inject"), ","))
		c.String(http.StatusOK, "hello %s", c.Param("name"))
	})
	server := httptest.NewServer(router)
	defer server.Close()
	client := server.Client()
	defer client.CloseIdleConnections()
	resp, err := client.Get(server.URL + "/hello/modules")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != 299 || string(body) != "hello modules" {
		return fmt.Errorf("response = %d %q, want 299 hello modules", resp.StatusCode, body)
	}
	for key, want := range map[string]string{
		"X-Seen-Inject":        "client",
		"X-Go-Inject-Response": "client",
		"X-Go-Inject-Variant":  strings.Join(strings.Split(strings.TrimPrefix(gin.Version, "v"), ".")[:2], "."),
		"X-Go-Inject-Calls":    "1",
		"X-Go-Inject-Private":  "1",
		"X-Go-Inject-Order":    "10",
	} {
		if got := resp.Header.Values(key); len(got) != 1 || got[0] != want {
			return fmt.Errorf("%s = %v, want exactly one %q", key, got, want)
		}
	}
	return nil
}

func main() {
	if err := check(); err != nil {
		panic(err)
	}
	fmt.Println("PASS external: module rules, aggregate, deduplication, selected Gin variant")
}
