//go:generate generate-inject -path ../
package main

import (
	_ "gin_generate_inject/inject/gin"
	_ "github.com/kakj-go/go-inject-trace-contrib/skywalking/github.com/gin-gonic/gin"
)
