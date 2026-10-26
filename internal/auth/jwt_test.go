package auth

import (
	"strings"
	"testing"
	"time"
)

func TestManager(t *testing.T) {
	manager := Manager{Secret: []byte(strings.Repeat("s", 32)), Issuer: "test", TTL: time.Hour}
	token, err := manager.Sign(42)
	if err != nil {
		t.Fatal(err)
	}

	id, err := manager.Verify(token)
	if err != nil || id != 42 {
		t.Fatalf("got id=%d err=%v", id, err)
	}

	_, err = manager.Verify(token + "x")
	if err == nil {
		t.Fatal("expected tampered signature to be rejected")
	}
}
