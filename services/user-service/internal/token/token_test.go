package token

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"

	"github.com/Yeswanth03/distributed-judge-engine/services/user-service/internal/model"
)

func newIssuer(t *testing.T, previous ...*ecdsa.PublicKey) (*Issuer, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	iss, err := NewIssuer(key, previous, "test-issuer", "test-audience", 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return iss, key
}

// verify checks a token the way the gateway does: with a key from the published JWKS, picked by kid.
func verify(t *testing.T, iss *Issuer, raw string) Claims {
	t.Helper()
	var set jose.JSONWebKeySet
	if err := json.Unmarshal(iss.JWKS(), &set); err != nil {
		t.Fatal(err)
	}
	tok, err := jwt.ParseSigned(raw, []jose.SignatureAlgorithm{jose.ES256})
	if err != nil {
		t.Fatal(err)
	}
	keys := set.Key(tok.Headers[0].KeyID)
	if len(keys) != 1 {
		t.Fatalf("kid %q not in the JWKS", tok.Headers[0].KeyID)
	}
	var c Claims
	if err := tok.Claims(keys[0].Key, &c); err != nil {
		t.Fatalf("signature does not verify: %v", err)
	}
	return c
}

func TestIssueVerifiesWithTheJWKS(t *testing.T) {
	iss, _ := newIssuer(t)
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	iss.now = func() time.Time { return now }
	u := &model.User{ID: uuid.New(), Role: model.RoleAdmin}
	sid := uuid.New()

	raw, exp, err := iss.Issue(u, sid)
	if err != nil {
		t.Fatal(err)
	}
	c := verify(t, iss, raw)
	if c.Subject != u.ID.String() || c.Role != model.RoleAdmin || c.SessionID != sid {
		t.Fatalf("unexpected claims %+v", c)
	}
	if err := c.Validate(jwt.Expected{Issuer: "test-issuer", AnyAudience: jwt.Audience{"test-audience"}, Time: now}); err != nil {
		t.Fatalf("claims must validate: %v", err)
	}
	if !exp.Equal(now.Add(10*time.Minute)) || !c.Expiry.Time().Equal(exp) {
		t.Fatalf("expiry %v, claim %v", exp, c.Expiry.Time())
	}
	// Past the default one-minute clock-skew allowance (the gateway allows the same).
	if err := c.Validate(jwt.Expected{Time: exp.Add(2 * time.Minute)}); err == nil {
		t.Fatal("an expired token must not validate")
	}
}

func TestJWKSKeepsPreviousKeysForRotation(t *testing.T) {
	old, oldKey := newIssuer(t)
	oldToken, _, _ := old.Issue(&model.User{ID: uuid.New(), Role: model.RoleUser}, uuid.New())

	rotated, _ := newIssuer(t, &oldKey.PublicKey)
	var set jose.JSONWebKeySet
	if err := json.Unmarshal(rotated.JWKS(), &set); err != nil {
		t.Fatal(err)
	}
	if len(set.Keys) != 2 {
		t.Fatalf("expected the new and the previous key, got %d", len(set.Keys))
	}
	for _, k := range set.Keys {
		if k.Algorithm != "ES256" || k.Use != "sig" || !k.IsPublic() {
			t.Fatalf("unexpected JWK %+v", k)
		}
	}
	verify(t, rotated, oldToken) // a token from before the rotation still verifies
}

func TestKeyIDIsStable(t *testing.T) {
	key, _ := GenerateKey()
	a, _ := KeyID(&key.PublicKey)
	b, _ := KeyID(&key.PublicKey)
	if a != b || a == "" {
		t.Fatalf("kid must be the same every time, got %q and %q", a, b)
	}
}

func TestRejectsKeysThatAreNotP256(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if _, err := NewIssuer(key, nil, "i", "a", time.Minute); err == nil {
		t.Fatal("a P-384 key must be refused: tokens are ES256")
	}
}

func TestPEMRoundTrip(t *testing.T) {
	key, _ := GenerateKey()
	encoded, err := EncodePrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParsePrivateKey(encoded)
	if err != nil || !parsed.Equal(key) {
		t.Fatalf("private key round trip failed: %v", err)
	}
	der, _ := x509.MarshalPKIXPublicKey(&key.PublicKey)
	pub, err := ParsePublicKey(string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})))
	if err != nil || !pub.Equal(&key.PublicKey) {
		t.Fatalf("public key round trip failed: %v", err)
	}
	if _, err := ParsePrivateKey("not pem"); err == nil {
		t.Fatal("garbage must be refused")
	}
}

func TestRefreshTokens(t *testing.T) {
	a, hashA, err := NewRefreshToken()
	if err != nil {
		t.Fatal(err)
	}
	b, _, _ := NewRefreshToken()
	if a == b || len(a) < 40 {
		t.Fatalf("refresh tokens must be long and random, got %q and %q", a, b)
	}
	if string(HashRefreshToken(a)) != string(hashA) {
		t.Fatal("the stored hash must match the token")
	}
}
