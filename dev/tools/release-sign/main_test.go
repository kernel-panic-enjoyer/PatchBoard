package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRunWritesDetachedSignature(t *testing.T) {
	_, privateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATCHBOARD_TEST_SIGNING_KEY", base64.StdEncoding.EncodeToString(privateKey))
	directory := t.TempDir()
	metadataPath := filepath.Join(directory, "PatchBoard.metadata.json")
	metadataData := []byte(`{"signing_key_id":"test-key","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`)
	if err := os.WriteFile(metadataPath, metadataData, 0o600); err != nil {
		t.Fatal(err)
	}
	outputPath := filepath.Join(directory, "PatchBoard.update-signature.json")
	if err := run(metadataPath, outputPath, "PATCHBOARD_TEST_SIGNING_KEY", "test-key"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	var result signature
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if result.ProtocolVersion != protocolVersion || result.KeyID != "test-key" || result.Signature == "" {
		t.Fatalf("unexpected signature: %#v", result)
	}
}
