# gin-toolexec-inject

### server start

```shell
make gin-toolexec-inject-server
cd example/gin-toolexec-inject/server && ./server
```

server 启动后和 [gin-generate-inject/server/server.go](..%2Fgin-generate-inject%2Fserver%2Fserver.go) 的日志相同

### client start

```shell
make gin-toolexec-inject-client
cd example/gin-toolexec-inject/client && ./clent
```

client 连接后日志和 [gin-generate-inject/client/client.go](..%2Fgin-generate-inject%2Fclient) 的日志不同，变更的日志如下。其原因是对 golang src 库进行了注入

```
req: aaa 
Response Received: {"message":"hello world change"}
```

client 连接后 server 端有以下新日志

```
before request
[GIN] 2023/08/09 - 15:23:29 | 200 |      39.041µs |             ::1 | POST     "/hello"
after request
```

对应代码注入位置可以看 [inject](inject) 中的代码

