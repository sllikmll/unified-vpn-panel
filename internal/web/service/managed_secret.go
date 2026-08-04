package service

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

const managedSecretEnvelopeVersion = 1

var (
	ErrManagedSecretUnavailable = errors.New("managed secret key unavailable")
	ErrManagedSecretCorrupt     = errors.New("managed secret envelope corrupt")
)

type ManagedSecretKeySource interface {
	MasterKey() ([]byte, string, error)
}

type ManagedSecretFileKeySource struct {
	Path string
}

type ManagedSecretStaticKeySource struct {
	Key   []byte
	KeyID string
}

type ManagedSecretEnvelopeService struct {
	Keys ManagedSecretKeySource
	Rand io.Reader
}

type ManagedSecretAAD struct {
	OwnerType  string
	OwnerId    int
	SecretKind string
}

func NewManagedSecretEnvelopeService(keys ManagedSecretKeySource) ManagedSecretEnvelopeService {
	return ManagedSecretEnvelopeService{Keys: keys, Rand: rand.Reader}
}

func (s ManagedSecretEnvelopeService) Encrypt(aad ManagedSecretAAD, plaintext []byte) (model.ManagedSecret, error) {
	if len(plaintext) == 0 {
		return model.ManagedSecret{}, fmt.Errorf("%w: empty plaintext", ErrManagedSecretCorrupt)
	}
	key, keyID, err := s.masterKey()
	if err != nil {
		return model.ManagedSecret{}, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return model.ManagedSecret{}, fmt.Errorf("%w: invalid key", ErrManagedSecretUnavailable)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return model.ManagedSecret{}, err
	}
	nonce := make([]byte, gcm.NonceSize())
	reader := s.Rand
	if reader == nil {
		reader = rand.Reader
	}
	if _, err := io.ReadFull(reader, nonce); err != nil {
		return model.ManagedSecret{}, err
	}
	out := model.ManagedSecret{
		OwnerType:       aad.OwnerType,
		OwnerId:         aad.OwnerId,
		SecretKind:      aad.SecretKind,
		EnvelopeVersion: managedSecretEnvelopeVersion,
		KeyID:           keyID,
		Nonce:           nonce,
	}
	out.Ciphertext = gcm.Seal(nil, nonce, plaintext, managedSecretAAD(aad, out.EnvelopeVersion, keyID))
	out.Fingerprint = managedSecretFingerprint(key, aad, plaintext)
	return out, nil
}

func (s ManagedSecretEnvelopeService) Decrypt(row model.ManagedSecret, aad ManagedSecretAAD) ([]byte, error) {
	if row.EnvelopeVersion != managedSecretEnvelopeVersion || row.KeyID == "" || len(row.Nonce) == 0 || len(row.Ciphertext) == 0 {
		return nil, ErrManagedSecretCorrupt
	}
	if row.OwnerType != aad.OwnerType || row.OwnerId != aad.OwnerId || row.SecretKind != aad.SecretKind {
		return nil, ErrManagedSecretCorrupt
	}
	key, keyID, err := s.masterKey()
	if err != nil {
		return nil, err
	}
	if keyID != row.KeyID {
		return nil, ErrManagedSecretCorrupt
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid key", ErrManagedSecretUnavailable)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(row.Nonce) != gcm.NonceSize() {
		return nil, ErrManagedSecretCorrupt
	}
	plaintext, err := gcm.Open(nil, row.Nonce, row.Ciphertext, managedSecretAAD(aad, row.EnvelopeVersion, row.KeyID))
	if err != nil {
		return nil, ErrManagedSecretCorrupt
	}
	return plaintext, nil
}

func (s ManagedSecretEnvelopeService) masterKey() ([]byte, string, error) {
	if s.Keys == nil {
		s.Keys = ManagedSecretFileKeySource{Path: DefaultManagedSecretMasterKeyPath()}
	}
	key, keyID, err := s.Keys.MasterKey()
	if err != nil {
		return nil, "", err
	}
	if len(key) != 32 {
		return nil, "", fmt.Errorf("%w: key must be 32 bytes", ErrManagedSecretUnavailable)
	}
	if keyID == "" {
		sum := sha256.Sum256(key)
		keyID = hex.EncodeToString(sum[:16])
	}
	return append([]byte(nil), key...), keyID, nil
}

func (s ManagedSecretStaticKeySource) MasterKey() ([]byte, string, error) {
	if len(s.Key) != 32 {
		return nil, "", fmt.Errorf("%w: static key must be 32 bytes", ErrManagedSecretUnavailable)
	}
	return append([]byte(nil), s.Key...), s.KeyID, nil
}

func (s ManagedSecretFileKeySource) MasterKey() ([]byte, string, error) {
	path := s.Path
	if path == "" {
		path = DefaultManagedSecretMasterKeyPath()
	}
	if st, err := os.Lstat(path); err == nil {
		if err := validateManagedSecretKeyFileInfo(st); err != nil {
			return nil, "", err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, "", ErrManagedSecretUnavailable
		}
		key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
		if err != nil || len(key) != 32 {
			return nil, "", ErrManagedSecretUnavailable
		}
		return key, "", nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, "", ErrManagedSecretUnavailable
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, "", ErrManagedSecretUnavailable
	}
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, "", ErrManagedSecretUnavailable
	}
	tmp, err := os.OpenFile(path+".tmp."+strconv.FormatInt(int64(os.Getpid()), 10), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, "", ErrManagedSecretUnavailable
	}
	if _, err := tmp.WriteString(base64.StdEncoding.EncodeToString(key) + "\n"); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return nil, "", ErrManagedSecretUnavailable
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return nil, "", ErrManagedSecretUnavailable
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return nil, "", ErrManagedSecretUnavailable
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		_ = os.Remove(tmp.Name())
		return nil, "", ErrManagedSecretUnavailable
	}
	if dir, err := os.Open(filepath.Dir(path)); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return key, "", nil
}

func validateManagedSecretKeyFileInfo(st os.FileInfo) error {
	if st.Mode()&os.ModeSymlink != 0 || !st.Mode().IsRegular() {
		return ErrManagedSecretUnavailable
	}
	return validateManagedSecretKeyFileSecurity(st)
}

func DefaultManagedSecretMasterKeyPath() string {
	if v := strings.TrimSpace(os.Getenv("XUI_MANAGED_SECRET_KEY_FILE")); v != "" {
		return v
	}
	return filepath.Join("/var/lib/x-ui", "managed-secret.master")
}

func managedSecretAAD(aad ManagedSecretAAD, version int, keyID string) []byte {
	return []byte(aad.OwnerType + "\x00" + strconv.Itoa(aad.OwnerId) + "\x00" + aad.SecretKind + "\x00" + strconv.Itoa(version) + "\x00" + keyID)
}

func managedSecretFingerprint(key []byte, aad ManagedSecretAAD, plaintext []byte) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(aad.OwnerType))
	mac.Write([]byte{0})
	mac.Write([]byte(strconv.Itoa(aad.OwnerId)))
	mac.Write([]byte{0})
	mac.Write([]byte(aad.SecretKind))
	mac.Write([]byte{0})
	mac.Write(plaintext)
	return hex.EncodeToString(mac.Sum(nil))
}
