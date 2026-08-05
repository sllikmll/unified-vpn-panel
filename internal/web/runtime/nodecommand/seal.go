package nodecommand

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
)

type sealedSecretEnvelope struct {
	Material []byte            `json:"material,omitempty"`
	Refs     map[string]string `json:"refs,omitempty"`
}

func SealMaterial(key, material []byte) (string, error) {
	if len(key) == 0 {
		return "", fmt.Errorf("%w: seal key", ErrMissingField)
	}
	if len(material) > MaxSecretMaterialBytes {
		return "", fmt.Errorf("%w: secret material", ErrInvalidField)
	}
	gcm, err := sealGCM(key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	out := append([]byte(nil), nonce...)
	out = gcm.Seal(out, nonce, material, nil)
	return base64.RawURLEncoding.EncodeToString(out), nil
}

func OpenSealedMaterial(key []byte, sealed string) ([]byte, error) {
	if len(key) == 0 {
		return nil, fmt.Errorf("%w: seal key", ErrMissingField)
	}
	if !isSafeSealedPayload(sealed) {
		return nil, fmt.Errorf("%w: sealedPayload", ErrInvalidField)
	}
	raw, err := base64.RawURLEncoding.DecodeString(sealed)
	if err != nil {
		return nil, fmt.Errorf("%w: sealedPayload", ErrInvalidField)
	}
	gcm, err := sealGCM(key)
	if err != nil {
		return nil, err
	}
	if len(raw) < gcm.NonceSize() {
		return nil, fmt.Errorf("%w: sealedPayload", ErrInvalidField)
	}
	nonce, body := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	opened, err := gcm.Open(nil, nonce, body, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: sealedPayload", ErrInvalidField)
	}
	if len(opened) > MaxSecretMaterialBytes {
		return nil, fmt.Errorf("%w: secret material", ErrInvalidField)
	}
	return opened, nil
}

func SealSecretInput(key []byte, secret *SecretInput) (string, error) {
	if secret == nil {
		return "", nil
	}
	if err := validateSecretInput(secret); err != nil {
		return "", err
	}
	raw, err := json.Marshal(sealedSecretEnvelope{Material: secret.Material, Refs: secret.Refs})
	if err != nil {
		return "", err
	}
	return SealMaterial(key, raw)
}

func OpenSealedSecretInput(key []byte, sealed string) (*SecretInput, error) {
	opened, err := OpenSealedMaterial(key, sealed)
	if err != nil {
		return nil, err
	}
	var env sealedSecretEnvelope
	if err := json.Unmarshal(opened, &env); err == nil && (env.Material != nil || env.Refs != nil) {
		secret := &SecretInput{Material: env.Material, Refs: env.Refs}
		if err := validateSecretInput(secret); err != nil {
			return nil, err
		}
		return secret, nil
	}
	secret := &SecretInput{Material: opened}
	if err := validateSecretInput(secret); err != nil {
		return nil, err
	}
	return secret, nil
}

func sealGCM(key []byte) (cipher.AEAD, error) {
	sum := sha256.Sum256(key)
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
