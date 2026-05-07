// Package license implements Ed25519-signed license files.
//
// Wire format: a single line of "<base64url-payload>.<base64url-signature>".
// The payload is a JSON document; the signature covers exactly the payload
// bytes (before base64 decoding). Same idea as a detached JWS without the
// alg header — keypair lives in the publisher's hands, not the file.
//
// Verification only requires the public key, which the server embeds via
// LICENSE_PUBLIC_KEY. Issuing requires the private key, which lives only
// with whoever sells the product (you).
package license

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// Payload is the JSON-serialized portion of a license. Be careful adding
// required fields: deployed servers may have older licenses without them, so
// new fields should be optional / behind feature flags.
type Payload struct {
	LicenseID   string    `json:"license_id"`
	IssuedTo    string    `json:"issued_to"`
	IssuedAt    time.Time `json:"issued_at"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	MaxProjects int       `json:"max_projects,omitempty"` // 0 = unlimited
	Features    []string  `json:"features,omitempty"`
}

// Status describes a license's current verification state at a point in time.
type Status struct {
	Valid      bool       `json:"valid"`
	Reason     string     `json:"reason,omitempty"` // empty when Valid
	IssuedTo   string     `json:"issued_to,omitempty"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	DaysLeft   int        `json:"days_left,omitempty"`
}

// Errors returned by Parse / Verify. Callers can check via errors.Is.
var (
	ErrMalformed = errors.New("license: malformed file")
	ErrSignature = errors.New("license: invalid signature")
	ErrExpired   = errors.New("license: expired")
)

// GenerateKeypair returns a fresh Ed25519 keypair, base64 encoded.
// The private key is what you keep; the public key is what ships with the
// server. Standard library Ed25519 already uses 32-byte seeds.
func GenerateKeypair() (publicB64, privateB64 string, err error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	return base64.StdEncoding.EncodeToString(pub),
		base64.StdEncoding.EncodeToString(priv),
		nil
}

// Issue signs a payload with the given base64-encoded Ed25519 private key
// and returns the encoded license file contents (single line).
func Issue(p Payload, privateKeyB64 string) (string, error) {
	privBytes, err := base64.StdEncoding.DecodeString(privateKeyB64)
	if err != nil {
		return "", fmt.Errorf("decode private key: %w", err)
	}
	if len(privBytes) != ed25519.PrivateKeySize {
		return "", fmt.Errorf("private key wrong size: want %d, got %d", ed25519.PrivateKeySize, len(privBytes))
	}
	priv := ed25519.PrivateKey(privBytes)

	payloadJSON, err := json.Marshal(p)
	if err != nil {
		return "", err
	}

	sig := ed25519.Sign(priv, payloadJSON)

	enc := base64.RawURLEncoding
	return enc.EncodeToString(payloadJSON) + "." + enc.EncodeToString(sig), nil
}

// Verify parses and verifies a license file's contents against the given
// base64-encoded Ed25519 public key. Returns the payload on success.
//
// Expiry is reported via ErrExpired but the payload is still returned —
// callers may want to surface a "renew now" UX rather than reject outright.
func Verify(contents, publicKeyB64 string) (*Payload, error) {
	pubBytes, err := base64.StdEncoding.DecodeString(publicKeyB64)
	if err != nil {
		return nil, fmt.Errorf("decode public key: %w", err)
	}
	if len(pubBytes) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("public key wrong size: want %d, got %d", ed25519.PublicKeySize, len(pubBytes))
	}
	pub := ed25519.PublicKey(pubBytes)

	contents = strings.TrimSpace(contents)
	parts := strings.SplitN(contents, ".", 2)
	if len(parts) != 2 {
		return nil, ErrMalformed
	}

	enc := base64.RawURLEncoding
	payloadJSON, err := enc.DecodeString(parts[0])
	if err != nil {
		return nil, ErrMalformed
	}
	sig, err := enc.DecodeString(parts[1])
	if err != nil {
		return nil, ErrMalformed
	}

	if !ed25519.Verify(pub, payloadJSON, sig) {
		return nil, ErrSignature
	}

	var p Payload
	if err := json.Unmarshal(payloadJSON, &p); err != nil {
		return &p, ErrMalformed
	}

	if p.ExpiresAt != nil && time.Now().After(*p.ExpiresAt) {
		return &p, ErrExpired
	}

	return &p, nil
}

// VerifyFile is a convenience wrapper that reads from disk.
func VerifyFile(path, publicKeyB64 string) (*Payload, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Verify(string(b), publicKeyB64)
}

// StatusFromVerify converts a Verify result into a UI-friendly Status.
// payload may be nil; err nil means valid.
func StatusFromVerify(payload *Payload, err error) Status {
	if err == nil {
		s := Status{Valid: true}
		if payload != nil {
			s.IssuedTo = payload.IssuedTo
			if payload.ExpiresAt != nil {
				s.ExpiresAt = payload.ExpiresAt
				s.DaysLeft = int(time.Until(*payload.ExpiresAt).Hours() / 24)
			}
		}
		return s
	}
	s := Status{Valid: false, Reason: err.Error()}
	if payload != nil {
		s.IssuedTo = payload.IssuedTo
		s.ExpiresAt = payload.ExpiresAt
	}
	return s
}
