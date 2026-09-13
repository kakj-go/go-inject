package main

import "testing"

func TestHTTP(t *testing.T) {
	if err := check(); err != nil {
		t.Fatal(err)
	}
}
