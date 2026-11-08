package monero

import "testing"

func TestNormalizeRPCURL(t *testing.T) {
	tests := map[string]string{
		"http://127.0.0.1:18088":           "http://127.0.0.1:18088",
		"http://127.0.0.1:18088/":          "http://127.0.0.1:18088",
		"http://127.0.0.1:18088/json_rpc":  "http://127.0.0.1:18088",
		"http://127.0.0.1:18088/json_rpc/": "http://127.0.0.1:18088",
		"  http://node.local:38088  ":      "http://node.local:38088",
	}
	for input, want := range tests {
		if got := NormalizeRPCURL(input); got != want {
			t.Fatalf("NormalizeRPCURL(%q) = %q, want %q", input, got, want)
		}
	}
}
