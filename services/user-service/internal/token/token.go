// Package token issues the access tokens (ES256 JWTs) the gateway verifies, publishes the matching public keys
// as a JWKS, and makes the opaque refresh tokens.
//
// The gateway checks access tokens on its own with the cached JWKS, so no request ever has to call this
// service to be authorised. That is what keeps the User Service out of every other request's critical path.
package token

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"

	"github.com/Yeswanth03/distributed-judge-engine/services/user-service/internal/model"
)

// Claims are the access token's contents (docs/contracts.md section 7). The gateway copies sub to X-User-Id
// and role to X-User-Role.
type Claims struct {
	jwt.Claims
	Role      model.Role `json:"role"`
	SessionID uuid.UUID  `json:"sid"`
}

// Issuer signs access tokens with the current key and publishes every key still in use.
type Issuer struct {
	key      *ecdsa.PrivateKey
	keyID    string
	signer   jose.Signer
	jwks     []byte
	issuer   string
	audience string
	ttl      time.Duration
	now      func() time.Time
}

// NewIssuer signs with key. previous are public keys of earlier signing keys: they stay in the JWKS until
// every token they signed has expired, so rotating the key logs nobody out.
func NewIssuer(key *ecdsa.PrivateKey, previous []*ecdsa.PublicKey, issuer, audience string, ttl time.Duration) (*Issuer, error) {
	if key.Curve != elliptic.P256() {
		return nil, errors.New("signing key must be an ECDSA P-256 key (ES256)")
	}
	kid, err := KeyID(&key.PublicKey)
	if err != nil {
		return nil, err
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.ES256, Key: jose.JSONWebKey{Key: key, KeyID: kid}},
		(&jose.SignerOptions{}).WithType("JWT"))
	if err != nil {
		return nil, err
	}
	set := jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: kid, Algorithm: string(jose.ES256), Use: "sig"}}}
	for _, pub := range previous {
		id, err := KeyID(pub)
		if err != nil {
			return nil, err
		}
		set.Keys = append(set.Keys, jose.JSONWebKey{Key: pub, KeyID: id, Algorithm: string(jose.ES256), Use: "sig"})
	}
	jwks, err := json.Marshal(set)
	if err != nil {
		return nil, err
	}
	return &Issuer{key: key, keyID: kid, signer: signer, jwks: jwks, issuer: issuer, audience: audience, ttl: ttl,
		now: time.Now}, nil
}

// Issue returns a signed access token for the user in this session, and when it expires.
func (i *Issuer) Issue(u *model.User, sessionID uuid.UUID) (string, time.Time, error) {
	now := i.now().UTC().Truncate(time.Second)
	exp := now.Add(i.ttl)
	claims := Claims{
		Claims: jwt.Claims{
			Issuer: i.issuer, Audience: jwt.Audience{i.audience}, Subject: u.ID.String(), ID: uuid.NewString(),
			IssuedAt: jwt.NewNumericDate(now), NotBefore: jwt.NewNumericDate(now), Expiry: jwt.NewNumericDate(exp),
		},
		Role:      u.Role,
		SessionID: sessionID,
	}
	raw, err := jwt.Signed(i.signer).Claims(claims).Serialize()
	return raw, exp, err
}

// JWKS is the public key set served at /.well-known/jwks.json.
func (i *Issuer) JWKS() []byte { return i.jwks }

// KeyID returns the key's RFC 7638 thumbprint, so the same key always has the same kid on every replica.
func KeyID(pub *ecdsa.PublicKey) (string, error) {
	tp, err := (&jose.JSONWebKey{Key: pub}).Thumbprint(crypto.SHA256)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(tp), nil
}

// NewRefreshToken returns an opaque refresh token for the client and the hash the database keeps. The token is
// 256 random bits, so a fast hash is enough: there is nothing to brute-force.
func NewRefreshToken() (string, []byte, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, err
	}
	t := base64.RawURLEncoding.EncodeToString(raw)
	return t, HashRefreshToken(t), nil
}

func HashRefreshToken(t string) []byte {
	sum := sha256.Sum256([]byte(t))
	return sum[:]
}

// GenerateKey makes a new P-256 signing key.
func GenerateKey() (*ecdsa.PrivateKey, error) {
	return ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
}

// EncodePrivateKey and ParsePrivateKey convert a key to and from PKCS#8 PEM.
func EncodePrivateKey(key *ecdsa.PrivateKey) (string, error) {
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})), nil
}

func ParsePrivateKey(pemText string) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemText))
	if block == nil {
		return nil, errors.New("no PEM block in private key")
	}
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		if ec, ecErr := x509.ParseECPrivateKey(block.Bytes); ecErr == nil {
			return ec, nil
		}
		return nil, fmt.Errorf("parse private key: %w", err)
	}
	ec, ok := k.(*ecdsa.PrivateKey)
	if !ok {
		return nil, errors.New("private key is not an ECDSA key")
	}
	return ec, nil
}

func ParsePublicKey(pemText string) (*ecdsa.PublicKey, error) {
	block, _ := pem.Decode([]byte(pemText))
	if block == nil {
		return nil, errors.New("no PEM block in public key")
	}
	k, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse public key: %w", err)
	}
	ec, ok := k.(*ecdsa.PublicKey)
	if !ok {
		return nil, errors.New("public key is not an ECDSA key")
	}
	return ec, nil
}
