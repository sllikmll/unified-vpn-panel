package naiveproxy

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const maxLocalCertificateBytes = 1 << 20

func PrepareLocalCertificate(endpoint *Endpoint, sourceRoot, destinationDir string, uid, gid int) (bool, error) {
	if endpoint == nil {
		return false, errors.New("naiveproxy endpoint is nil")
	}
	endpoint.CertificateFile = ""
	endpoint.KeyFile = ""
	if err := endpoint.Validate(); err != nil {
		return false, err
	}
	domain := canonicalDomain(endpoint.Domain)
	sourceDir := filepath.Join(sourceRoot, "live", domain)
	certSource := filepath.Join(sourceDir, "fullchain.pem")
	keySource := filepath.Join(sourceDir, "privkey.pem")
	certExists := pathExists(certSource)
	keyExists := pathExists(keySource)
	if !certExists && !keyExists {
		return false, nil
	}
	if !certExists || !keyExists {
		return false, errors.New("naiveproxy local certificate pair is incomplete")
	}
	rootReal, err := filepath.EvalSymlinks(sourceRoot)
	if err != nil {
		return false, errors.New("resolve naiveproxy certificate root failed")
	}
	certReal, err := confinedCertificatePath(rootReal, certSource)
	if err != nil {
		return false, err
	}
	keyReal, err := confinedCertificatePath(rootReal, keySource)
	if err != nil {
		return false, err
	}
	certPEM, err := readBoundedRegularFile(certReal)
	if err != nil {
		return false, err
	}
	keyPEM, err := readBoundedRegularFile(keyReal)
	if err != nil {
		return false, err
	}
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil || len(pair.Certificate) == 0 {
		return false, errors.New("validate naiveproxy local certificate keypair failed")
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil || time.Now().Before(leaf.NotBefore) || !time.Now().Before(leaf.NotAfter) || leaf.VerifyHostname(domain) != nil {
		return false, errors.New("validate naiveproxy local certificate identity failed")
	}
	if err := os.MkdirAll(destinationDir, 0o700); err != nil {
		return false, errors.New("create naiveproxy certificate directory failed")
	}
	if err := os.Chown(destinationDir, uid, gid); err != nil {
		return false, errors.New("set naiveproxy certificate directory owner failed")
	}
	certDestination := filepath.Join(destinationDir, "fullchain.pem")
	keyDestination := filepath.Join(destinationDir, "privkey.pem")
	if err := atomicOwnedFile(certDestination, certPEM, 0o644, uid, gid); err != nil {
		return false, err
	}
	if err := atomicOwnedFile(keyDestination, keyPEM, 0o600, uid, gid); err != nil {
		return false, err
	}
	endpoint.CertificateFile = certDestination
	endpoint.KeyFile = keyDestination
	return true, nil
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func confinedCertificatePath(rootReal, path string) (string, error) {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", errors.New("resolve naiveproxy certificate path failed")
	}
	rel, err := filepath.Rel(rootReal, real)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return "", errors.New("naiveproxy certificate path escapes managed root")
	}
	return real, nil
}

func readBoundedRegularFile(path string) ([]byte, error) {
	st, err := os.Stat(path)
	if err != nil || !st.Mode().IsRegular() || st.Size() <= 0 || st.Size() > maxLocalCertificateBytes {
		return nil, errors.New("naiveproxy certificate file is invalid")
	}
	return os.ReadFile(path)
}

func atomicOwnedFile(path string, content []byte, mode os.FileMode, uid, gid int) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".naiveproxy-cert-*")
	if err != nil {
		return errors.New("create naiveproxy certificate temp file failed")
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return errors.New("write naiveproxy certificate failed")
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return errors.New("set naiveproxy certificate mode failed")
	}
	if err := tmp.Chown(uid, gid); err != nil {
		_ = tmp.Close()
		return errors.New("set naiveproxy certificate owner failed")
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return errors.New("sync naiveproxy certificate failed")
	}
	if err := tmp.Close(); err != nil {
		return errors.New("close naiveproxy certificate failed")
	}
	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("replace naiveproxy certificate failed: %w", err)
	}
	return nil
}
