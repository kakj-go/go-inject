package main

import "testing"

func TestExternal(t *testing.T) {
	if err := check(); err != nil {
		t.Fatal(err)
	}
}
