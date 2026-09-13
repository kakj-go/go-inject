# HTTP 客户端

[English](README.md) · [全部示例](../README_CN.md)

[HTTP 规则](../rules/http/client.go)拦截 `net/http.(*Client).Do`。先克隆请求，再增加 `X-Go-Inject: client`；随后将返回响应改为 `299 Injected`，增加响应头。规则不读取或替换 body。

```sh
go mod tidy
go-inject test .
go-inject build -work -o http-example .
./http-example
```

Windows 使用 `-o http-example.exe` 和 `./http-example.exe`。

预期输出：

```text
PASS http: request header, response result, original body
```

程序通过 `httptest` 在自动分配的本地端口启动真实服务，发送请求，检查服务端收到的头、返回状态及响应头、原始 body，以及调用方请求未被修改。响应 body、空闲连接和服务自动关闭。

`net/http` 属于标准库，因此 `go-inject vendor .` 必须失败。这条规则使用构建后端，不涉及追踪运行时或远程遥测服务。
