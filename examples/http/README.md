# HTTP client

[简体中文](README_CN.md) · [All examples](../README.md)

The [HTTP rule](../rules/http/client.go) intercepts `net/http.(*Client).Do`. It clones the request before adding `X-Go-Inject: client`, then changes the returned response to status `299 Injected` and adds a response header. It does not read or replace the body.

```sh
go mod tidy
go test -toolexec="go-inject" .
go build -toolexec="go-inject" -work -o http-example .
./http-example
```

Windows: use `-o http-example.exe` and `./http-example.exe`.

Expected output:

```text
PASS http: request header, response result, original body
```

The program starts a real local `httptest` server on an automatically assigned port, sends a request, and checks the server-observed header, returned status/header, original body, and unchanged caller request. It closes the response body, idle connections, and server.

`go generate .` must fail because `net/http` belongs to the standard library. Use the build backend for this rule. Runtime tracing or a remote telemetry service is not involved.
