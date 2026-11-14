package password

import (
	"strings"
	"testing"
)

var fast = Params{MemoryKiB: 64, Iterations: 1, Threads: 1}

func TestHashAndVerify(t *testing.T) {
	h, err := Hash("correct horse battery", fast)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=64,t=1,p=1$") {
		t.Fatalf("unexpected encoding %q", h)
	}
	if ok, err := Verify("correct horse battery", h); err != nil || !ok {
		t.Fatalf("the right password must verify: %v %v", ok, err)
	}
	if ok, _ := Verify("correct horse battery!", h); ok {
		t.Fatal("a wrong password must not verify")
	}
}

func TestSaltsDiffer(t *testing.T) {
	a, _ := Hash("same password", fast)
	b, _ := Hash("same password", fast)
	if a == b {
		t.Fatal("two hashes of the same password must differ (random salt)")
	}
}

func TestVerifyUsesTheStoredParameters(t *testing.T) {
	// A hash made with other settings still verifies: parameters can be raised without breaking old hashes.
	h, _ := Hash("pw-123456", Params{MemoryKiB: 128, Iterations: 2, Threads: 1})
	if ok, err := Verify("pw-123456", h); err != nil || !ok {
		t.Fatalf("got %v %v", ok, err)
	}
}

func TestMalformedHash(t *testing.T) {
	for _, h := range []string{"", "plain", "$argon2i$v=19$m=64,t=1,p=1$c2FsdA$aGFzaA", "$argon2id$v=19$m=x$c2FsdA$aGFzaA",
		"$argon2id$v=19$m=64,t=1,p=1$!!$aGFzaA"} {
		if _, err := Verify("x", h); err == nil {
			t.Errorf("%q: expected ErrMalformedHash", h)
		}
	}
}

func TestBurnDoesNotPanic(t *testing.T) {
	Burn("anything")
}
