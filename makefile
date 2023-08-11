GOPATH ?= $(shell go env GOPATH)

all: generate-inject toolexec-inject


gin-toolexec-inject-server:
	cd example/gin-toolexec-inject && go mod tidy && cd server && go build -a -toolexec="toolexec-inject -path ../"

gin-toolexec-inject-client:
	cd example/gin-toolexec-inject && go mod tidy && cd client && go build -a -toolexec="toolexec-inject -path ../"

gin-generate-inject-server:
	cd example/gin-generate-inject/server  && go generate && go build -a .

gin-generate-inject-client:
	cd example/gin-generate-inject/client && go build -a .

generate-inject:
	cd tools/generate-inject && go mod tidy && go build -a . && chmod +x generate-inject && mv generate-inject $(GOPATH)/bin/generate-inject

toolexec-inject:
	cd tools/toolexec-inject && go mod tidy && go build -a . && chmod +x toolexec-inject && mv toolexec-inject $(GOPATH)/bin/toolexec-inject