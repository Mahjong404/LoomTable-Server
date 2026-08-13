package auth

import "testing"

func TestVerifyBearer(t *testing.T) {
	hash := HashToken("test-token")
	if !VerifyBearer("Bearer test-token", hash) {
		t.Fatal("VerifyBearer() rejected the expected token")
	}
	if VerifyBearer("Bearer wrong-token", hash) {
		t.Fatal("VerifyBearer() accepted the wrong token")
	}
	if VerifyBearer("Basic test-token", hash) {
		t.Fatal("VerifyBearer() accepted a non-Bearer credential")
	}
}
