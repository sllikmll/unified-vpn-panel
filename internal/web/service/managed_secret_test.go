package service

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
)

func testSecretService(key byte) ManagedSecretEnvelopeService {
	k := bytes.Repeat([]byte{key}, 32)
	return NewManagedSecretEnvelopeService(ManagedSecretStaticKeySource{Key: k, KeyID: "test-key"})
}

func TestManagedSecretEnvelopeRoundTripAndAAD(t *testing.T) {
	svc := testSecretService(7)
	aad := ManagedSecretAAD{OwnerType: "managed_endpoint", OwnerId: 1, SecretKind: "server.privateKey"}
	row, err := svc.Encrypt(aad, []byte("secret-value"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	got, err := svc.Decrypt(row, aad)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if string(got) != "secret-value" {
		t.Fatalf("plaintext = %q", got)
	}
	if _, err := svc.Decrypt(row, ManagedSecretAAD{OwnerType: aad.OwnerType, OwnerId: 2, SecretKind: aad.SecretKind}); err == nil {
		t.Fatal("Decrypt with wrong owner succeeded")
	}
	if _, err := testSecretService(8).Decrypt(row, aad); err == nil {
		t.Fatal("Decrypt with wrong key succeeded")
	}
	row.Ciphertext[0] ^= 0xff
	if _, err := svc.Decrypt(row, aad); err == nil {
		t.Fatal("Decrypt corrupted ciphertext succeeded")
	}
}

func TestManagedSecretFingerprintIncludesOwner(t *testing.T) {
	svc := testSecretService(9)
	a, err := svc.Encrypt(ManagedSecretAAD{OwnerType: "managed_endpoint", OwnerId: 1, SecretKind: "k"}, []byte("same"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.Encrypt(ManagedSecretAAD{OwnerType: "managed_endpoint", OwnerId: 2, SecretKind: "k"}, []byte("same"))
	if err != nil {
		t.Fatal(err)
	}
	if a.Fingerprint == b.Fingerprint {
		t.Fatal("fingerprint reveals equality across owners")
	}
}

func TestManagedSecretFileKeyPersistsAndMode0600(t *testing.T) {
	path := filepath.Join(t.TempDir(), "managed-secret.master")
	src := ManagedSecretFileKeySource{Path: path}
	key1, _, err := src.MasterKey()
	if err != nil {
		t.Fatalf("first MasterKey: %v", err)
	}
	key2, _, err := src.MasterKey()
	if err != nil {
		t.Fatalf("second MasterKey: %v", err)
	}
	if !bytes.Equal(key1, key2) {
		t.Fatal("master key did not persist across restart")
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 0600", st.Mode().Perm())
	}
}

func TestManagedSecretNoPlaintextSQLiteScanAndJSONRedaction(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "x-ui.db")); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	svc := testSecretService(10)
	row, err := svc.Encrypt(ManagedSecretAAD{OwnerType: "managed_endpoint", OwnerId: 99, SecretKind: "k"}, []byte("sqlite-secret-value"))
	if err != nil {
		t.Fatal(err)
	}
	if err := database.GetDB().Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "sqlite-secret-value") || strings.Contains(string(raw), string(row.Ciphertext)) {
		t.Fatalf("ManagedSecret JSON leaked secret material: %s", raw)
	}
	sqlDB, err := database.GetDB().DB()
	if err != nil {
		t.Fatal(err)
	}
	var dump string
	if err := sqlDB.QueryRow("SELECT group_concat(CAST(ciphertext AS TEXT), '') FROM managed_secrets").Scan(&dump); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(dump, "sqlite-secret-value") {
		t.Fatal("SQLite ciphertext scan found plaintext")
	}
}

func TestManagedSecretRejectsCorruptFileKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "managed-secret.master")
	if err := os.WriteFile(path, []byte("not-base64"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := (ManagedSecretFileKeySource{Path: path}).MasterKey(); err == nil {
		t.Fatal("corrupt file key was accepted")
	}
}

func TestManagedSecretRejectsSymlinkAndBroadPermissions(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("not-a-key"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "managed-secret.master")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, _, err := (ManagedSecretFileKeySource{Path: link}).MasterKey(); err == nil {
		t.Fatal("symlink key file was accepted")
	}

	broad := filepath.Join(t.TempDir(), "managed-secret.master")
	if err := os.WriteFile(broad, []byte(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := (ManagedSecretFileKeySource{Path: broad}).MasterKey(); err == nil {
		t.Fatal("broadly-readable key file was accepted")
	}
}
