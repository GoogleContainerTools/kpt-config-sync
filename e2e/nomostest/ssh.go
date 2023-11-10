// Copyright 2022 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package nomostest

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"kpt.dev/configsync/e2e"
	"kpt.dev/configsync/e2e/nomostest/gitproviders"
	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/reconcilermanager/controllers"
)

const (
	// rootAuthSecretName is the name of the Auth secret required by the
	// RootSync reconciler to authenticate with the git-server.
	rootAuthSecretName = controllers.GitCredentialVolume

	// rootCACertSecretName is the name of the CACert secret required by
	// the RootSync reconciler.
	rootCACertSecretName = "git-cert-pub"

	// NamespaceAuthSecretName is the name of the Auth secret required by the
	// RepoSync reconciler to authenticate with the git-server.
	NamespaceAuthSecretName = "ssh-key"

	// NamespaceCACertSecretName is the name of the CACert secret required by
	// the RepoSync reconciler to authenticate the git-server SSL cert.
	NamespaceCACertSecretName = "git-cert-pub"

	// gitServerSecretName is the name of the Secret used by the local
	// git-server to authenticate clients.
	gitServerSecretName = "ssh-pub"

	// gitServerCertSecretName ins the name of the Secret used by the local
	// git-server to authenticate itself to clients.
	gitServerCertSecretName = "ssl-cert"
)

func sshDir(nt *NT) string {
	return filepath.Join(nt.TmpDir, "ssh")
}

func sslDir(nt *NT) string {
	return filepath.Join(nt.TmpDir, "ssl")
}

func privateKeyPath(nt *NT) string {
	return filepath.Join(sshDir(nt), "id_rsa.nomos")
}

func publicKeyPath(nt *NT) string {
	return filepath.Join(sshDir(nt), "id_rsa.nomos.pub")
}

func caCertPath(nt *NT) string {
	return filepath.Join(sslDir(nt), "ca_cert.pem")
}

func certPath(nt *NT) string {
	return filepath.Join(sslDir(nt), "cert.pem")
}

func certPrivateKeyPath(nt *NT) string {
	return filepath.Join(sslDir(nt), "key.pem")
}

// createSSHKeySecret generates a public/public key pair for the test.
func createSSHKeyPair(nt *NT) error {
	if err := os.MkdirAll(sshDir(nt), fileMode); err != nil {
		return fmt.Errorf("creating ssh directory: %w", err)
	}
	// ssh-keygen -t rsa -b 4096 -N "" \
	//   -f /opt/testing/nomos/id_rsa.nomos
	//   -C "key generated for use in e2e tests"
	_, err := nt.Shell.ExecWithDebug("ssh-keygen", "-t", "rsa", "-b", "4096", "-N", "",
		"-f", privateKeyPath(nt),
		"-C", "key generated for use in e2e tests")
	if err != nil {
		return fmt.Errorf("generating rsa key for ssh: %w", err)
	}
	return nil
}

func writePEMToFile(path, pemType string, data []byte) error {
	pemBuffer := new(bytes.Buffer)
	err := pem.Encode(pemBuffer, &pem.Block{
		Type:  pemType,
		Bytes: data,
	})
	if err != nil {
		return fmt.Errorf("encoding pem: %w", err)
	}
	if err = os.WriteFile(path, pemBuffer.Bytes(), 0644); err != nil {
		return fmt.Errorf("writing pem file: %s: %w", path, err)
	}
	return nil
}

func createCAWithCerts(nt *NT) error {
	if err := os.MkdirAll(sslDir(nt), fileMode); err != nil {
		return fmt.Errorf("creating ssl directory: %w", err)
	}
	ca := &x509.Certificate{
		SerialNumber:          big.NewInt(1984),
		Subject:               pkix.Name{CommonName: "localhost"},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().AddDate(1, 0, 0),
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	caPrivateKey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return fmt.Errorf("creating ca private key: %w", err)
	}
	caBytes, err := x509.CreateCertificate(rand.Reader, ca, ca, &caPrivateKey.PublicKey, caPrivateKey)
	if err != nil {
		return fmt.Errorf("creating ca cert: %w", err)
	}

	cert := &x509.Certificate{
		SerialNumber: big.NewInt(2112),
		Subject:      pkix.Name{CommonName: "localhost"},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().AddDate(1, 0, 0),
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"test-git-server.config-management-system-test"},
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	certPrivateKey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return fmt.Errorf("creating server private key: %w", err)
	}
	certBytes, err := x509.CreateCertificate(rand.Reader, cert, ca, &certPrivateKey.PublicKey, caPrivateKey)
	if err != nil {
		return fmt.Errorf("creating server cert: %w", err)
	}

	if err := writePEMToFile(caCertPath(nt), "CERTIFICATE", caBytes); err != nil {
		return err
	}
	if err := writePEMToFile(certPath(nt), "CERTIFICATE", certBytes); err != nil {
		return err
	}
	return writePEMToFile(certPrivateKeyPath(nt), "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(certPrivateKey))
}

// createSecret creates secret in the given namespace using 'keypath'.
func createSecret(nt *NT, namespace, name string, keyPaths ...string) error {
	if err := nt.KubeClient.Get(name, namespace, &corev1.Secret{}); err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("getting secret: %w", err)
		}
		// not found -> create
		args := []string{
			"create", "secret", "generic", name, "-n", namespace,
		}
		for _, kp := range keyPaths {
			args = append(args, "--from-file", kp)
		}
		_, err := nt.Shell.Kubectl(args...)
		if err != nil {
			return fmt.Errorf("creating secret: %w", err)
		}
	}
	// else found, don't re-create
	return nil
}

// generateSSHKeys generates a public/public key pair for the test.
//
// It turns out kubectl create secret is annoying to emulate, and it doesn't
// expose the inner logic to outside consumers. So instead of trying to do it
// ourselves, we're shelling out to kubectl to ensure we create a valid set of
// secrets.
func generateSSHKeys(nt *NT) (string, error) {
	if err := createSSHKeyPair(nt); err != nil {
		return "", err
	}

	if err := createSecret(nt, configmanagement.ControllerNamespace, rootAuthSecretName,
		fmt.Sprintf("ssh=%s", privateKeyPath(nt))); err != nil {
		return "", err
	}

	if err := createSecret(nt, testGitNamespace, gitServerSecretName,
		publicKeyPath(nt)); err != nil {
		return "", err
	}

	return privateKeyPath(nt), nil
}

// generateSSLKeys generates a self signed certificate for the test
//
// It turns out kubectl create secret is annoying to emulate, and it doesn't
// expose the inner logic to outside consumers. So instead of trying to do it
// ourselves, we're shelling out to kubectl to ensure we create a valid set of
// secrets.
func generateSSLKeys(nt *NT) (string, error) {
	if err := createCAWithCerts(nt); err != nil {
		return "", err
	}

	if err := createSecret(nt, configmanagement.ControllerNamespace, rootCACertSecretName,
		fmt.Sprintf("cert=%s", caCertPath(nt))); err != nil {
		return "", err
	}

	if err := createSecret(nt, testGitNamespace, gitServerCertSecretName,
		fmt.Sprintf("server.crt=%s", certPath(nt)),
		fmt.Sprintf("server.key=%s", certPrivateKeyPath(nt))); err != nil {
		return "", err
	}

	return caCertPath(nt), nil
}

// downloadSSHKey downloads the private SSH key from Cloud Secret Manager.
func downloadSSHKey(nt *NT) (string, error) {
	dir := sshDir(nt)
	if err := os.MkdirAll(dir, fileMode); err != nil {
		return "", fmt.Errorf("creating ssh directory: %w", err)
	}

	out, err := gitproviders.FetchCloudSecret(gitproviders.PrivateSSHKey)
	if err != nil {
		return "", fmt.Errorf("downloading SSH key: %w", err)
	}

	if err := os.WriteFile(privateKeyPath(nt), []byte(out), 0600); err != nil {
		return "", fmt.Errorf("saving SSH key: %w", err)
	}

	if err := createSecret(nt, configmanagement.ControllerNamespace, rootAuthSecretName,
		fmt.Sprintf("ssh=%s", privateKeyPath(nt))); err != nil {
		return "", err
	}

	return privateKeyPath(nt), nil
}

// CreateNamespaceSecret creates secrets in a given namespace using local paths.
func CreateNamespaceSecret(nt *NT, ns string) error {
	privateKeypath := nt.gitPrivateKeyPath
	if len(privateKeypath) == 0 {
		privateKeypath = privateKeyPath(nt)
	}
	if err := createSecret(nt, ns, NamespaceAuthSecretName, fmt.Sprintf("ssh=%s", privateKeypath)); err != nil {
		return err
	}
	if nt.GitProvider.Type() == e2e.Local {
		caCertPathVal := nt.caCertPath
		if len(caCertPathVal) == 0 {
			caCertPathVal = caCertPath(nt)
		}
		if err := createSecret(nt, ns, NamespaceCACertSecretName, fmt.Sprintf("cert=%s", caCertPathVal)); err != nil {
			return err
		}
	}
	return nil
}
