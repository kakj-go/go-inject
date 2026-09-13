package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

func check() error {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.GET("/hello/:name", func(c *gin.Context) { c.String(http.StatusOK, "hello %s", c.Param("name")) })
	server := httptest.NewServer(router)
	defer server.Close()
	client := server.Client()
	defer client.CloseIdleConnections()
	for call := 1; call <= 2; call++ {
		resp, err := client.Get(server.URL + "/hello/reader")
		if err != nil {
			return err
		}
		body, readErr := io.ReadAll(resp.Body)
		closeErr := resp.Body.Close()
		if readErr != nil {
			return readErr
		}
		if closeErr != nil {
			return closeErr
		}
		if resp.StatusCode != http.StatusOK || string(body) != "hello reader" {
			return fmt.Errorf("response = %d %q, want 200 hello reader", resp.StatusCode, body)
		}
		want := map[string]string{
			"X-Go-Inject-Calls":   strconv.Itoa(call),
			"X-Go-Inject-Private": "1",
			"X-Go-Inject-Order":   "10,20",
			"X-Go-Inject-Variant": strings.Join(strings.Split(strings.TrimPrefix(gin.Version, "v"), ".")[:2], "."),
		}
		for key, value := range want {
			if got := resp.Header.Get(key); got != value {
				return fmt.Errorf("%s = %q, want %q", key, got, value)
			}
		}
	}
	return nil
}

func main() {
	if err := check(); err != nil {
		panic(err)
	}
	fmt.Println("PASS gin: private method, private field, added field, helper, order, variant")
}
