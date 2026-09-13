//go:build goinject || generate

package main

//go:generate go-inject vendor .

import (
	_ "github.com/kakj-go/go-inject/examples/rules/all"
	_ "github.com/kakj-go/go-inject/examples/rules/http"
)
