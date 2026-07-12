package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
)

const protocolVersion = 1

type metadata struct {
	SigningKeyID string `json:"signing_key_id"`
	SHA256       string `json:"sha256"`
}

type signature struct {
	ProtocolVersion int    `json:"protocol_version"`
	KeyID           string `json:"key_id"`
	MetadataSHA256  string `json:"metadata_sha256"`
	ArtifactSHA256  string `json:"artifact_sha256"`
	Signature       string `json:"signature"`
}

func main() {
	metadataPath := flag.String("metadata", "", "release metadata path")
	outputPath := flag.String("output", "", "signature output path")
	privateKeyEnv := flag.String("private-key-env", "PATCHBOARD_UPDATE_SIGNING_PRIVATE_KEY", "environment variable containing a base64 Ed25519 private key or seed")
	keyID := flag.String("key-id", "", "expected signing key ID")
	flag.Parse()
	if err := run(*metadataPath, *outputPath, *privateKeyEnv, *keyID); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(metadataPath, outputPath, privateKeyEnv, expectedKeyID string) error {
	if metadataPath == "" || outputPath == "" || expectedKeyID == "" {
		return errors.New("metadata, output, and key-id are required")
	}
	metadataBytes, err := os.ReadFile(metadataPath)
	if err != nil {
		return err
	}
	var releaseMetadata metadata
	decoder := json.NewDecoder(strings.NewReader(string(metadataBytes)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&releaseMetadata); err != nil {
		return err
	}
	if releaseMetadata.SigningKeyID != expectedKeyID || releaseMetadata.SHA256 == "" {
		return errors.New("release metadata does not match the configured signing key")
	}
	privateKeyBytes, err := base64.StdEncoding.DecodeString(strings.TrimSpace(os.Getenv(privateKeyEnv)))
	if err != nil {
		return fmt.Errorf("decode %s: %w", privateKeyEnv, err)
	}
	var privateKey ed25519.PrivateKey
	switch len(privateKeyBytes) {
	case ed25519.SeedSize:
		privateKey = ed25519.NewKeyFromSeed(privateKeyBytes)
	case ed25519.PrivateKeySize:
		privateKey = ed25519.PrivateKey(privateKeyBytes)
	default:
		return errors.New("release signing private key has an invalid length")
	}
	metadataDigest := sha256.Sum256(metadataBytes)
	metadataSHA256 := hex.EncodeToString(metadataDigest[:])
	artifactSHA256 := strings.ToLower(strings.TrimSpace(releaseMetadata.SHA256))
	message := []byte("PatchBoard self-update signature v1\nmetadata_sha256=" + metadataSHA256 + "\nartifact_sha256=" + artifactSHA256 + "\n")
	payload, err := json.Marshal(signature{
		ProtocolVersion: protocolVersion,
		KeyID:           expectedKeyID,
		MetadataSHA256:  metadataSHA256,
		ArtifactSHA256:  artifactSHA256,
		Signature:       base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, message)),
	})
	if err != nil {
		return err
	}
	return os.WriteFile(outputPath, payload, 0o600)
}
