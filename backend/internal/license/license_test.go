package license

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRoundTrip(t *testing.T) {
	pub, priv, err := GenerateKeypair()
	if err != nil {
		t.Fatal(err)
	}

	p := Payload{
		LicenseID: "lic-1",
		IssuedTo:  "Acme Corp",
		IssuedAt:  time.Now().Truncate(time.Second),
	}
	file, err := Issue(p, priv)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(file, ".") {
		t.Fatalf("expected payload.signature format, got %q", file)
	}

	got, err := Verify(file, pub)
	if err != nil {
		t.Fatal(err)
	}
	if got.IssuedTo != "Acme Corp" {
		t.Errorf("IssuedTo = %q, want Acme Corp", got.IssuedTo)
	}
}

func TestVerify_Tampered(t *testing.T) {
	pub, priv, err := GenerateKeypair()
	if err != nil {
		t.Fatal(err)
	}
	file, _ := Issue(Payload{LicenseID: "x", IssuedTo: "A", IssuedAt: time.Now()}, priv)

	// Flip a byte in the signature half.
	parts := strings.SplitN(file, ".", 2)
	tampered := parts[0] + "." + flipFirst(parts[1])

	_, err = Verify(tampered, pub)
	if !errors.Is(err, ErrSignature) {
		t.Errorf("expected ErrSignature, got %v", err)
	}
}

func TestVerify_TamperedPayload(t *testing.T) {
	pub, priv, err := GenerateKeypair()
	if err != nil {
		t.Fatal(err)
	}
	file, _ := Issue(Payload{LicenseID: "x", IssuedTo: "A", IssuedAt: time.Now()}, priv)
	parts := strings.SplitN(file, ".", 2)
	tampered := flipFirst(parts[0]) + "." + parts[1]

	_, err = Verify(tampered, pub)
	if err == nil {
		t.Error("expected error from tampered payload, got nil")
	}
}

func TestVerify_Expired(t *testing.T) {
	pub, priv, err := GenerateKeypair()
	if err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-24 * time.Hour)
	file, _ := Issue(Payload{
		LicenseID: "x",
		IssuedTo:  "A",
		IssuedAt:  time.Now().Add(-48 * time.Hour),
		ExpiresAt: &past,
	}, priv)

	got, err := Verify(file, pub)
	if !errors.Is(err, ErrExpired) {
		t.Fatalf("expected ErrExpired, got %v", err)
	}
	// Payload still returned so UI can show "renew this license"
	if got == nil || got.IssuedTo != "A" {
		t.Errorf("payload should still be returned with ErrExpired, got %v", got)
	}
}

func TestVerify_Malformed(t *testing.T) {
	pub, _, _ := GenerateKeypair()
	cases := []string{"", "no-dot", "....", "@.@"}
	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			_, err := Verify(c, pub)
			if err == nil {
				t.Errorf("expected error for %q", c)
			}
		})
	}
}

func TestVerify_WrongKey(t *testing.T) {
	_, priv, _ := GenerateKeypair()
	otherPub, _, _ := GenerateKeypair()
	file, _ := Issue(Payload{LicenseID: "x", IssuedTo: "A", IssuedAt: time.Now()}, priv)

	_, err := Verify(file, otherPub)
	if !errors.Is(err, ErrSignature) {
		t.Errorf("expected ErrSignature when verifying with different public key, got %v", err)
	}
}

func flipFirst(s string) string {
	if s == "" {
		return s
	}
	first := s[0]
	if first == 'A' {
		first = 'B'
	} else {
		first = 'A'
	}
	return string(first) + s[1:]
}
