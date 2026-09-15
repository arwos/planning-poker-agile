package httpapi

import (
	"net/http"
	"testing"
)

func TestRemoteIPUsesForwardedAddressOnlyFromLoopbackProxy(t *testing.T) {
	request := &http.Request{RemoteAddr: "127.0.0.1:8080", Header: http.Header{"X-Forwarded-For": {"198.51.100.10"}}}
	if got := remoteIP(request); got != "198.51.100.10" {
		t.Fatalf("remoteIP=%q, want forwarded client address", got)
	}

	request.RemoteAddr = "198.51.100.20:8080"
	request.Header.Set("X-Forwarded-For", "203.0.113.20")
	if got := remoteIP(request); got != "198.51.100.20" {
		t.Fatalf("remoteIP=%q, want direct peer address", got)
	}
}
