package updater

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const selfUpdateSignatureProtocolVersion = 1

type selfUpdateSignature struct {
	ProtocolVersion int    `json:"protocol_version"`
	KeyID           string `json:"key_id"`
	MetadataSHA256  string `json:"metadata_sha256"`
	ArtifactSHA256  string `json:"artifact_sha256"`
	Signature       string `json:"signature"`
}

func selfUpdateSignatureMessage(metadataSHA256, artifactSHA256 string) []byte {
	return []byte("PatchBoard self-update signature v1\nmetadata_sha256=" + strings.ToLower(metadataSHA256) + "\nartifact_sha256=" + strings.ToLower(artifactSHA256) + "\n")
}

func trustedSelfUpdateSigningKeys() (map[string]ed25519.PublicKey, error) {
	keys := make(map[string]ed25519.PublicKey)
	for _, pair := range strings.FieldsFunc(appUpdateTrustedSigningKeys, func(r rune) bool { return r == ',' || r == ';' || r == '\n' }) {
		parts := strings.SplitN(strings.TrimSpace(pair), "=", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" {
			return nil, errors.New("trusted self-update signing key list is malformed")
		}
		keyID := strings.TrimSpace(parts[0])
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(parts[1]))
		if err != nil || len(decoded) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("trusted self-update signing key %q is invalid", keyID)
		}
		if _, exists := keys[keyID]; exists {
			return nil, fmt.Errorf("trusted self-update signing key %q is duplicated", keyID)
		}
		keys[keyID] = ed25519.PublicKey(decoded)
	}
	if len(keys) == 0 {
		return nil, errors.New("this build has no trusted self-update signing keys")
	}
	if primaryKeyID := strings.TrimSpace(appUpdateSigningKeyID); primaryKeyID != "" {
		if _, ok := keys[primaryKeyID]; !ok {
			return nil, fmt.Errorf("configured self-update signing key %q is not in the trusted key list", primaryKeyID)
		}
	}
	return keys, nil
}

func decodeSelfUpdateSignature(data []byte) (selfUpdateSignature, error) {
	var signature selfUpdateSignature
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&signature); err != nil {
		return signature, err
	}
	if signature.ProtocolVersion != selfUpdateSignatureProtocolVersion || strings.TrimSpace(signature.KeyID) == "" || !sha256LinePattern.MatchString(signature.MetadataSHA256) || !sha256LinePattern.MatchString(signature.ArtifactSHA256) || strings.TrimSpace(signature.Signature) == "" {
		return signature, errors.New("self-update signature is invalid")
	}
	return signature, nil
}

func validateSelfUpdateSignature(signature selfUpdateSignature, metadataData []byte, artifactSHA256 string) error {
	keys, err := trustedSelfUpdateSigningKeys()
	if err != nil {
		return err
	}
	publicKey, ok := keys[signature.KeyID]
	if !ok {
		return fmt.Errorf("self-update signature key %q is not trusted", signature.KeyID)
	}
	metadataDigest := sha256.Sum256(metadataData)
	metadataSHA256 := hex.EncodeToString(metadataDigest[:])
	if !strings.EqualFold(signature.MetadataSHA256, metadataSHA256) {
		return errors.New("self-update signature metadata digest mismatch")
	}
	if !strings.EqualFold(signature.ArtifactSHA256, artifactSHA256) {
		return errors.New("self-update signature artifact digest mismatch")
	}
	encodedSignature, err := base64.StdEncoding.DecodeString(signature.Signature)
	if err != nil || len(encodedSignature) != ed25519.SignatureSize {
		return errors.New("self-update signature encoding is invalid")
	}
	if !ed25519.Verify(publicKey, selfUpdateSignatureMessage(metadataSHA256, artifactSHA256), encodedSignature) {
		return errors.New("self-update signature verification failed")
	}
	return nil
}
