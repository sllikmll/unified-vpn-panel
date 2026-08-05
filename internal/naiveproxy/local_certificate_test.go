package naiveproxy

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPrepareLocalCertificateCopiesValidatedPairAtomically(t *testing.T) {
	root := t.TempDir()
	domain := "naive.example.test"
	archive := filepath.Join(root, "archive", domain)
	live := filepath.Join(root, "live", domain)
	if err := os.MkdirAll(archive, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(live, 0o700); err != nil {
		t.Fatal(err)
	}
	certPEM, keyPEM := testCertificatePair(t, domain)
	if err := os.WriteFile(filepath.Join(archive, "fullchain1.pem"), certPEM, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(archive, "privkey1.pem"), keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..", "..", "archive", domain, "fullchain1.pem"), filepath.Join(live, "fullchain.pem")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..", "..", "archive", domain, "privkey1.pem"), filepath.Join(live, "privkey.pem")); err != nil {
		t.Fatal(err)
	}
	endpoint := Endpoint{Domain: domain, ListenIP: "127.0.0.1", Port: 32003, ACMEEmail: "ops@example.test"}
	dest := filepath.Join(t.TempDir(), "tls")
	used, err := PrepareLocalCertificate(&endpoint, root, dest, os.Getuid(), os.Getgid())
	if err != nil || !used {
		t.Fatalf("used=%v err=%v", used, err)
	}
	if endpoint.CertificateFile != filepath.Join(dest, "fullchain.pem") || endpoint.KeyFile != filepath.Join(dest, "privkey.pem") {
		t.Fatalf("endpoint paths=%+v", endpoint)
	}
	if st, err := os.Stat(endpoint.KeyFile); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("key mode=%v err=%v", st, err)
	}
}

func TestPrepareLocalCertificateRejectsEscapingSymlink(t *testing.T) {
	root := t.TempDir()
	domain := "naive.example.test"
	live := filepath.Join(root, "live", domain)
	if err := os.MkdirAll(live, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	certPEM, keyPEM := testCertificatePair(t, domain)
	cert := filepath.Join(outside, "fullchain.pem")
	key := filepath.Join(outside, "privkey.pem")
	if err := os.WriteFile(cert, certPEM, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(key, keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(cert, filepath.Join(live, "fullchain.pem")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(key, filepath.Join(live, "privkey.pem")); err != nil {
		t.Fatal(err)
	}
	endpoint := Endpoint{Domain: domain, ListenIP: "127.0.0.1", Port: 32003, ACMEEmail: "ops@example.test"}
	if _, err := PrepareLocalCertificate(&endpoint, root, filepath.Join(t.TempDir(), "tls"), os.Getuid(), os.Getgid()); err == nil {
		t.Fatal("expected symlink escape error")
	}
}

func testCertificatePair(t *testing.T, domain string) ([]byte, []byte) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	tpl := x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: domain}, DNSNames: []string{domain}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, &tpl, &tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
}
