# gin-generator-inject

### server start

```shell
make gin-generate-inject-server
cd example/gin-generate-inject/server && ./server
```

server 启动后可以看到以下日志, 其注入的内容对应 [generate.go](server%2Fgenerate.go) 中声明的包的内容

```log
before Default
[GIN-debug] [WARNING] Creating an Engine instance with the Logger and Recovery middleware already attached.

[GIN-debug] [WARNING] Running in "debug" mode. Switch to "release" mode in production.
 - using env:   export GIN_MODE=release
 - using code:  gin.SetMode(gin.ReleaseMode)

after Default
Handlers len: 2
before post
[GIN-debug] POST   /hello                    --> main.main.func1 (3 handlers)
after post 
[GIN-debug] [WARNING] You trusted all proxies, this is NOT safe. We recommend you to set a value.
Please check https://pkg.go.dev/github.com/gin-gonic/gin#readme-don-t-trust-all-proxies for details.
[GIN-debug] Environment variable PORT is undefined. Using port :8080 by default
[GIN-debug] Listening and serving HTTP on :8080
```

### client start 

```shell
make gin-generate-inject-client
cd example/gin-generate-inject/client && ./clent
```

client 日志

```
Response Received: {"message":"hello world"}
```

client 连接后 server 端有以下新日志

```
before request
[GIN] 2023/08/09 - 15:23:29 | 200 |      39.041µs |             ::1 | POST     "/hello"
after request
```

### 代码修改位置

可以打开 vendor 看以下代码变更位置

vendor/github.com/gin-gonic/gin/gin.go 148 行

vendor/github.com/gin-gonic/gin/gin.go 482 行

vendor/github.com/gin-gonic/gin/routergroup.go 86 行

