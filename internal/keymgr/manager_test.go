package keymgr

import (
	"strings"
	"testing"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/nip19"
)

func TestParseSecretHex(t *testing.T) {
	hexSecret := strings.Repeat("1", 64)

	sk, err := ParseSecret(hexSecret)
	if err != nil {
		t.Fatalf("ParseSecret() error = %v", err)
	}

	if got := sk.Hex(); got != hexSecret {
		t.Fatalf("expected hex %q, got %q", hexSecret, got)
	}
}

func TestParseSecretNsec(t *testing.T) {
	hexSecret := strings.Repeat("2", 64)
	sk, err := nostr.SecretKeyFromHex(hexSecret)
	if err != nil {
		t.Fatalf("SecretKeyFromHex() setup error = %v", err)
	}

	nsec := nip19.EncodeNsec(sk)
	got, err := ParseSecret(nsec)
	if err != nil {
		t.Fatalf("ParseSecret() error = %v", err)
	}

	if got.Hex() != hexSecret {
		t.Fatalf("expected key %q, got %q", hexSecret, got.Hex())
	}
}

func TestParseSecretInvalid(t *testing.T) {
	tests := []string{
		"",
		"  ",
		"deadbeef",
		strings.Repeat("z", 64),
		"npub1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq",
	}

	for _, tc := range tests {
		t.Run(tc, func(t *testing.T) {
			_, err := ParseSecret(tc)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if err != ErrInvalidSecret {
				t.Fatalf("expected ErrInvalidSecret, got %v", err)
			}
		})
	}
}

func TestManagerImportAndCurrentIdentity(t *testing.T) {
	hexSecret := strings.Repeat("3", 64)
	sk, err := nostr.SecretKeyFromHex(hexSecret)
	if err != nil {
		t.Fatalf("SecretKeyFromHex() setup error = %v", err)
	}

	expectedPK := sk.Public()
	expectedNPub := nip19.EncodeNpub(expectedPK)

	m := New()
	id, err := m.Import(hexSecret)
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}

	if !m.HasSecret() {
		t.Fatal("expected manager to have secret after import")
	}

	if id.PubKeyHex != expectedPK.Hex() {
		t.Fatalf("expected pubkey %q, got %q", expectedPK.Hex(), id.PubKeyHex)
	}

	if id.NPub != expectedNPub {
		t.Fatalf("expected npub %q, got %q", expectedNPub, id.NPub)
	}

	current, err := m.CurrentIdentity()
	if err != nil {
		t.Fatalf("CurrentIdentity() error = %v", err)
	}

	if current != id {
		t.Fatalf("expected current identity %+v, got %+v", id, current)
	}
}

func TestManagerGenerateAndClear(t *testing.T) {
	m := New()

	id, err := m.Generate()
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	if id.PubKeyHex == "" || id.NPub == "" {
		t.Fatalf("expected non-empty identity, got %+v", id)
	}

	if !strings.HasPrefix(id.NPub, "npub1") {
		t.Fatalf("expected npub prefix, got %q", id.NPub)
	}

	if !m.HasSecret() {
		t.Fatal("expected manager to have secret after generate")
	}

	m.Clear()

	if m.HasSecret() {
		t.Fatal("expected manager to be empty after clear")
	}

	_, err = m.CurrentIdentity()
	if err != ErrNoSecret {
		t.Fatalf("expected ErrNoSecret after clear, got %v", err)
	}
}
