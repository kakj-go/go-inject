# gin-toolexec-inject

### server start

```shell
make gin-toolexec-inject-server
cd example/gin-toolexec-inject/server && ./server
```

The logs of the server after startup are same like [gin-generate-inject/server/server.go](..%2Fgin-generate-inject%2Fserver%2Fserver.go)

### client start

```shell
make gin-toolexec-inject-client
cd example/gin-toolexec-inject/client && ./clent
```

The client logs will be different

The log of changes is as follows. The reason is that the Golang src library was injected

```
req: aaa 
Response Received: {"message":"hello world change"}
```

The server has the following new logs after the client

```
before request
[GIN] 2023/08/09 - 15:23:29 | 200 |      39.041µs |             ::1 | POST     "/hello"
after request
```

The content of the corresponding code injection can be seen in the code in the [inject](inject)

