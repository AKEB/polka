package server

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	jose "github.com/go-jose/go-jose/v4"
)

func TestOIDCKeySetHS256(t *testing.T) {
	secret := []byte("super-secret-client-at-least-32b!") // HS256 needs ≥256-bit key
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.HS256, Key: secret}, nil)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]any{
		"iss": "https://idp.example",
		"sub": "user-1",
		"aud": "polka",
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Unix(),
	})
	obj, err := signer.Sign(payload)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := obj.CompactSerialize()
	if err != nil {
		t.Fatal(err)
	}

	ks := &oidcKeySet{secret: secret}
	got, err := ks.VerifySignature(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatalf("payload mismatch: %s", got)
	}

	bad := &oidcKeySet{secret: []byte("wrong-secret-also-32-bytes-long!!")}
	if _, err := bad.VerifySignature(context.Background(), raw); err == nil {
		t.Fatal("wrong secret must fail")
	}

	empty := &oidcKeySet{}
	if _, err := empty.VerifySignature(context.Background(), raw); err == nil {
		t.Fatal("missing secret must fail for HS256")
	}
}

func TestOIDCVerifierAcceptsHS256(t *testing.T) {
	secret := []byte("client-secret-must-be-32-bytes!!")
	const issuer = "https://idp.example"
	const clientID = "polka"

	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.HS256, Key: secret}, nil)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]any{
		"iss":   issuer,
		"sub":   "sub-42",
		"aud":   clientID,
		"exp":   time.Now().Add(time.Hour).Unix(),
		"iat":   time.Now().Unix(),
		"nonce": "n1",
		"email": "a@example.com",
	})
	obj, err := signer.Sign(payload)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := obj.CompactSerialize()
	if err != nil {
		t.Fatal(err)
	}

	v := oidc.NewVerifier(issuer, &oidcKeySet{secret: secret}, &oidc.Config{
		ClientID:             clientID,
		SupportedSigningAlgs: []string{"HS256", oidc.RS256},
	})
	tok, err := v.Verify(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if tok.Subject != "sub-42" {
		t.Fatalf("subject: %q", tok.Subject)
	}
}
