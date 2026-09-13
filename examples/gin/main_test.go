package main

import "testing"

func TestGin(t *testing.T) {
	if err := check(); err != nil {
		t.Fatal(err)
	}
}
