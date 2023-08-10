# gin-generator-inject

### server start

```shell
make gin-generate-inject-server
cd example/gin-generate-inject/server && ./server
```

After the server starts, you can see the following log, which injects the contents of the [generate.go](server%2Fgenerate.go)

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

client log

```
Response Received: {"message":"hello world"}
```

The server has the following new logs after the client

```
before request
[GIN] 2023/08/09 - 15:23:29 | 200 |      39.041µs |             ::1 | POST     "/hello"
after request
```

### Place of code change

You can open the vendor to view the following code changes

vendor/github.com/gin-gonic/gin/gin.go 148 line

vendor/github.com/gin-gonic/gin/gin.go 482 line

vendor/github.com/gin-gonic/gin/routergroup.go 86 line

