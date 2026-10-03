package auth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"

	"systemsfoundry/platform"
)

var errMissingClaims = errors.New("missing access claims")
var errBadSigningKey = errors.New("signing key must be a PEM encoded P-256 private key")

type Signer struct {
	key *ecdsa.PrivateKey
	kid string
	iss string
}

type accessExtra struct {
	TenantID string `json:"tenant_id"`
	Role     string `json:"role"`
	Nonce    string `json:"nonce,omitempty"`
}

func NewSigner(issuer string) (*Signer, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	return &Signer{key: key, kid: "foundry-1", iss: issuer}, nil
}

func NewSignerFromPEM(issuer string, keyPEM []byte) (*Signer, error) {
	block, _ := pem.Decode(keyPEM)
	if block == nil {
		return nil, errBadSigningKey
	}
	var key *ecdsa.PrivateKey
	if parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		ecKey, ok := parsed.(*ecdsa.PrivateKey)
		if !ok {
			return nil, errBadSigningKey
		}
		key = ecKey
	} else if key, err = x509.ParseECPrivateKey(block.Bytes); err != nil {
		return nil, errBadSigningKey
	}
	if key.Curve != elliptic.P256() {
		return nil, errBadSigningKey
	}
	publicDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(publicDER)
	return &Signer{key: key, kid: hex.EncodeToString(sum[:8]), iss: issuer}, nil
}

func (s *Signer) Sign(subject, tenantID, role, audience, nonce string, issued, expiry time.Time) (string, error) {
	options := (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", s.kid)
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.ES256, Key: s.key}, options)
	if err != nil {
		return "", err
	}
	claims := jwt.Claims{
		Issuer:   s.iss,
		Subject:  subject,
		Audience: jwt.Audience{audience},
		IssuedAt: jwt.NewNumericDate(issued),
		Expiry:   jwt.NewNumericDate(expiry),
	}
	extra := accessExtra{TenantID: tenantID, Role: role, Nonce: nonce}
	return jwt.Signed(signer).Claims(claims).Claims(extra).Serialize()
}

func (s *Signer) Verify(raw, audience string, now time.Time) (platform.Actor, error) {
	parsed, err := jwt.ParseSigned(raw, []jose.SignatureAlgorithm{jose.ES256})
	if err != nil {
		return platform.Actor{}, err
	}
	var claims jwt.Claims
	var extra accessExtra
	if err = parsed.Claims(&s.key.PublicKey, &claims, &extra); err != nil {
		return platform.Actor{}, err
	}
	err = claims.ValidateWithLeeway(jwt.Expected{
		Issuer:      s.iss,
		AnyAudience: jwt.Audience{audience},
		Time:        now,
	}, 0)
	if err != nil {
		return platform.Actor{}, err
	}
	if claims.Subject == "" || extra.TenantID == "" || extra.Role == "" {
		return platform.Actor{}, errMissingClaims
	}
	return platform.Actor{Subject: claims.Subject, TenantID: extra.TenantID, Role: extra.Role}, nil
}

func (s *Signer) JWKS() ([]byte, error) {
	set := jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
		Key:       &s.key.PublicKey,
		KeyID:     s.kid,
		Algorithm: string(jose.ES256),
		Use:       "sig",
	}}}
	return json.Marshal(set)
}
