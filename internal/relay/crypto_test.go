package relay

import (
	"os"
	"testing"

	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip44"
)

func TestCrypto_NIP44RoundTrip(t *testing.T) {
	laptopSK := nostr.GeneratePrivateKey()
	laptopPK, _ := nostr.GetPublicKey(laptopSK)

	phoneSK := nostr.GeneratePrivateKey()
	phonePK, _ := nostr.GetPublicKey(phoneSK)

	// Phone encrypts for Laptop
	phoneConvKey, err := nip44.GenerateConversationKey(laptopPK, phoneSK)
	if err != nil {
		t.Fatalf("Failed to generate phone conv key: %v", err)
	}

	plaintext := `{"action":"telemetry","id":"req-test-123"}`
	ciphertext, err := nip44.Encrypt(plaintext, phoneConvKey)
	if err != nil {
		t.Fatalf("NIP44 Encrypt failed: %v", err)
	}

	// Laptop derives conv key from Phone PK and Laptop SK
	laptopConvKey, err := nip44.GenerateConversationKey(phonePK, laptopSK)
	if err != nil {
		t.Fatalf("Failed to generate laptop conv key: %v", err)
	}

	// Verify both keys match
	if phoneConvKey != laptopConvKey {
		t.Fatal("Conversation keys derived by phone and laptop must be identical")
	}

	decrypted, err := nip44.Decrypt(ciphertext, laptopConvKey)
	if err != nil {
		t.Fatalf("NIP44 Decrypt failed: %v", err)
	}

	if decrypted != plaintext {
		t.Fatalf("Decrypted content mismatch! Got %s, expected %s", decrypted, plaintext)
	}
}

func TestCrypto_EventSignature(t *testing.T) {
	sk := nostr.GeneratePrivateKey()
	pk, _ := nostr.GetPublicKey(sk)

	evt := nostr.Event{
		PubKey:    pk,
		CreatedAt: nostr.Now(),
		Kind:      4,
		Tags:      nostr.Tags{{"p", "recipient-key"}},
		Content:   "encrypted-data-blob",
	}

	if err := evt.Sign(sk); err != nil {
		t.Fatalf("Sign failed: %v", err)
	}

	valid, err := evt.CheckSignature()
	if err != nil || !valid {
		t.Fatalf("Signature verification failed: %v", err)
	}

	// Tampering test: modified content must invalidate signature
	evt.Content = "tampered-content"
	valid, _ = evt.CheckSignature()
	if valid {
		t.Fatal("Tampered event MUST fail signature verification")
	}
}

func TestCrypto_NIP44Interop(t *testing.T) {
	laptopSK := "84587001171ccebb8e45dcf9fc1a35963ccd6e4e324dcaa0ada9c15465b831bc"
	phoneSK := "1111111111111111111111111111111111111111111111111111111111111111"
	phonePK, _ := nostr.GetPublicKey(phoneSK)

	convKey, err := nip44.GenerateConversationKey(phonePK, laptopSK)
	if err != nil {
		t.Fatalf("Failed to generate convKey: %v", err)
	}

	ctBytes, err := os.ReadFile("../../test_ct.txt")
	if err != nil {
		t.Skip("test_ct.txt not found, skipping JS interop test")
	}

	pt, err := nip44.Decrypt(string(ctBytes), convKey)
	if err != nil {
		t.Fatalf("Failed to decrypt JS-generated NIP-44 ciphertext: %v", err)
	}

	if pt != "Test Interop Payload" {
		t.Fatalf("Interop plaintext mismatch! Got %s, expected 'Test Interop Payload'", pt)
	}
}
