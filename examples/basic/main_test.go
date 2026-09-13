package main

import "testing"

func TestQuote(t *testing.T) {
	for _, tt := range []struct{ units, want int }{{2, 35}, {-1, 0}, {0, 15}} {
		if got := Quote(tt.units); got != tt.want {
			t.Errorf("Quote(%d) = %d, want %d; run this test with go-inject test .", tt.units, got, tt.want)
		}
	}
}
