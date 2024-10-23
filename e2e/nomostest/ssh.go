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
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"kpt.dev/configsync/e2e"
	"kpt.dev/configsync/e2e/nomostest/gitproviders"
	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/reconcilermanager/controllers"
)

// SyncSource represents a type of sync source. This typically maps to a type of
// server which hosts a source of truth for syncing. This does not necessarily
// map 1:1 with SourceType, for example both helm and oci use registry-server.
type SyncSource string

const (
	// RootAuthSecretName is the name of the Auth secret required by the
	// RootSync reconciler to authenticate with the git-server.
	RootAuthSecretName = controllers.GitCredentialVolume

	// NamespaceAuthSecretName is the name of the Auth secret required by the
	// RepoSync reconciler to authenticate with the git-server.
	NamespaceAuthSecretName = "ssh-key"

	// gitServerSecretName is the name of the Secret used by the local
	// git-server to authenticate clients.
	gitServerSecretName = "ssh-pub"

	// cosignSecretName is the name of the Secret that stores
	// the public key used by the pre-sync webhook server to verify OCI images.
	cosignSecretName = "cosign-key"

	// OCISignatureVerificationSecretName is the name of the Secret
	// that stores the TLS certificates for the pre-sync webhook server.
	OCISignatureVerificationSecretName = "oci-image-verification-key"

	// GitSyncSource is the name of the git-server sync source. Used by the git
	// source type.
	GitSyncSource SyncSource = "git-server"

	// RegistrySyncSource is the name of the registry-server sync source. Used
	// by both the oci and helm source types.
	RegistrySyncSource SyncSource = "registry-server"

	cosignPassword       = "test"
	cosignPasswordEnvVar = "COSIGN_PASSWORD"
)

func sshDir(nt *NT) string {
	return filepath.Join(nt.TmpDir, "ssh")
}

func sslDir(nt *NT, syncSource SyncSource) string {
	return filepath.Join(nt.TmpDir, string(syncSource), "ssl")
}

func cosignDir(nt *NT) string {
	return filepath.Join(nt.TmpDir, "cosign")
}

// CosignPrivateKeyPath returns the private key for signing image
func CosignPrivateKeyPath(nt *NT) string {
	return filepath.Join(cosignDir(nt), "cosign.key")
}

func cosignPublicKeyPath(nt *NT) string {
	return filepath.Join(cosignDir(nt), "cosign.pub")
}

func tlsDir(nt *NT) string {
	return filepath.Join(nt.TmpDir, "tls")
}

func tlsKeyPath(nt *NT) string {
	return filepath.Join(tlsDir(nt), "tls.key")
}

func tlsCertPath(nt *NT) string {
	return filepath.Join(tlsDir(nt), "tls.crt")
}

func privateKeyPath(nt *NT) string {
	return filepath.Join(sshDir(nt), "id_rsa.nomos")
}

func publicKeyPath(nt *NT) string {
	return filepath.Join(sshDir(nt), "id_rsa.nomos.pub")
}

func caCertPath(nt *NT, syncSource SyncSource) string {
	return filepath.Join(sslDir(nt, syncSource), "ca_cert.pem")
}

func certPath(nt *NT, syncSource SyncSource) string {
	return filepath.Join(sslDir(nt, syncSource), "cert.pem")
}

func certPrivateKeyPath(nt *NT, syncSource SyncSource) string {
	return filepath.Join(sslDir(nt, syncSource), "key.pem")
}

// GetKnownHosts will generate and format the key to be used for
// known_hosts authentication with local git provider
func GetKnownHosts(nt *NT) (string, error) {
	provider := nt.GitProvider.(*gitproviders.LocalProvider)
	port, err := provider.PortForwarder.LocalPort()
	if err != nil {
		return "", err
	}
	out, err := nt.Shell.ExecWithDebug("ssh-keyscan",
		"-t", "rsa",
		"-p", fmt.Sprintf("%d", port),
		"localhost")
	if err != nil {
		return "", err
	}
	output := string(out)
	// Replace the port-forwarded address at localhost with the in-cluster Service.
	// The git-sync container runs in cluster and communicates with the Service.
	knownHost := strings.Replace(output,
		fmt.Sprintf("[localhost]:%d", port),
		fmt.Sprintf("%s.%s", testGitServer, testGitNamespace), 1)
	return knownHost, nil
}

// createSSHKeySecret generates a public/public key pair for the test.
func createSSHKeyPair(nt *NT) error {
	if err := os.MkdirAll(sshDir(nt), fileMode); err != nil {
		return fmt.Errorf("creating ssh directory: %w", err)
	}
	filePath := privateKeyPath(nt)
	// ssh-keygen -t rsa -b 4096 -N "" \
	//   -f /opt/testing/nomos/id_rsa.nomos
	//   -C "key generated for use in e2e tests"
	out, err := nt.Shell.ExecWithDebug("ssh-keygen", "-t", "rsa", "-b", "4096", "-N", "",
		"-f", filePath,
		"-C", "key generated for use in e2e tests")
	if err != nil {
		return fmt.Errorf("generating rsa key for ssh: %w", err)
	}
	// Verify the key file was created
	if _, err := os.Stat(filePath); err != nil {
		// Log the ssh-keygen output. Maybe it says why it failed.
		nt.Logger.Infof("ERROR: failed to generate rsa key pair:\n%s", string(out))
		return fmt.Errorf("reading rsa key file: %s: %w", filePath, err)
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

func createCAWithCerts(nt *NT, syncSource SyncSource, domains []string) error {
	if err := os.MkdirAll(sslDir(nt, syncSource), fileMode); err != nil {
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
		DNSNames:     domains,
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

	if err := writePEMToFile(caCertPath(nt, syncSource), "CERTIFICATE", caBytes); err != nil {
		return err
	}
	if err := writePEMToFile(certPath(nt, syncSource), "CERTIFICATE", certBytes); err != nil {
		return err
	}
	return writePEMToFile(certPrivateKeyPath(nt, syncSource), "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(certPrivateKey))
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

	if err := createSecret(nt, configmanagement.ControllerNamespace, RootAuthSecretName,
		fmt.Sprintf("ssh=%s", privateKeyPath(nt))); err != nil {
		return "", err
	}

	if err := createSecret(nt, testGitNamespace, gitServerSecretName,
		publicKeyPath(nt)); err != nil {
		return "", err
	}

	return privateKeyPath(nt), nil
}

// PublicCertSecretName is the name of the Secret which contains a CA cert used
// for HTTPS handshakes of the provided sync source
func PublicCertSecretName(sourceType SyncSource) string {
	return fmt.Sprintf("%s-cert-public", sourceType)
}

// privateCertSecretName is the name of the Secret which contains the private
// certificate and key used by in-cluster sync sources for HTTPS handshakes.
// Example usages: git-server, registry-server
func privateCertSecretName(sourceType SyncSource) string {
	return fmt.Sprintf("%s-cert-private", sourceType)
}

// generateSSLKeys generates a self signed certificate for the test
//
// It turns out kubectl create secret is annoying to emulate, and it doesn't
// expose the inner logic to outside consumers. So instead of trying to do it
// ourselves, we're shelling out to kubectl to ensure we create a valid set of
// secrets.
func generateSSLKeys(nt *NT, syncSource SyncSource, namespace string, domains []string) (string, error) {
	if err := createCAWithCerts(nt, syncSource, domains); err != nil {
		return "", err
	}

	// Create public secret in config-management-system to enable syncing with
	// RootSyncs. For RepoSyncs, see CreateNamespaceSecrets
	if err := createSecret(nt, configmanagement.ControllerNamespace, PublicCertSecretName(syncSource),
		fmt.Sprintf("cert=%s", caCertPath(nt, syncSource))); err != nil {
		return "", err
	}

	if err := createSecret(nt, namespace, privateCertSecretName(syncSource),
		fmt.Sprintf("server.crt=%s", certPath(nt, syncSource)),
		fmt.Sprintf("server.key=%s", certPrivateKeyPath(nt, syncSource))); err != nil {
		return "", err
	}

	return caCertPath(nt, syncSource), nil
}

// generateCosignKeyPair generates a Cosign key pair using the `cosign` command
// and moves the generated keys to the specified directory. The two files will
// be named as cosign.key and cosign.pub. Subject to change based on Cosign.
func generateCosignKeyPair(nt *NT) error {
	if err := os.MkdirAll(cosignDir(nt), fileMode); err != nil {
		return fmt.Errorf("creating cosign key pair directory: %w", err)
	}

	err := os.Setenv(cosignPasswordEnvVar, cosignPassword)
	if err != nil {
		return err
	}
	defer func() {
		err := os.Unsetenv(cosignPasswordEnvVar)
		if err != nil {
			nt.T.Errorf("Failed to unset COSIGN_PASSWORD: %v", err)
		}
	}()
	nt.T.Log("Generating Cosign key pair...")
	cmd := exec.Command("cosign", "generate-key-pair")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("error running cosign command: %v", err)
	}

	nt.T.Log("Cosign key pair generated.")

	err = moveFile("cosign.key", CosignPrivateKeyPath(nt))
	if err != nil {
		return fmt.Errorf("failed to move cosign.key: %v", err)
	}

	err = moveFile("cosign.pub", cosignPublicKeyPath(nt))
	if err != nil {
		return fmt.Errorf("failed to move cosign.key: %v", err)
	}

	nt.T.Logf("Moved Cosign keys to %s\n", cosignDir(nt))
	return nil
}

func moveFile(src, dest string) error {
	err := os.MkdirAll(filepath.Dir(dest), fileMode)
	if err != nil {
		return fmt.Errorf("failed to create directory: %v", err)
	}

	err = os.Rename(src, dest)
	if err != nil {
		return fmt.Errorf("failed to move file from %s to %s: %v", src, dest, err)
	}
	return nil
}

// createKubernetesSecret creates a Kubernetes secret with the given name, namespace, type, and optional extra arguments.
func createKubernetesSecret(nt *NT, secretName, namespace, secretType string, args ...string) error {
	nt.T.Logf("Creating Kubernetes secret: %s\n", secretName)

	baseArgs := []string{"create", "secret", secretType, secretName}
	baseArgs = append(baseArgs, args...)
	baseArgs = append(baseArgs, "-n", namespace)
	_, err := nt.Shell.Kubectl(baseArgs...)
	if err != nil {
		return err
	}

	nt.T.Logf("Secret %s created.\n", secretName)
	return nil
}

// generateTLSKeyPair generates a TLS key pair using OpenSSL and saves the
// certificate and key to files.
func generateTLSKeyPair(nt *NT) error {
	if err := os.MkdirAll(tlsDir(nt), fileMode); err != nil {
		return fmt.Errorf("creating tls directory: %w", err)
	}

	nt.T.Log("Generating TLS key pair...")
	cmd := exec.Command("openssl", "req", "-nodes", "-x509", "-sha256", "-newkey", "rsa:4096",
		"-keyout", tlsKeyPath(nt), "-out", tlsCertPath(nt), "-days", "365",
		"-subj", "/CN=oci-signature-verification-server.oci-image-verification.svc",
		"-addext", "subjectAltName = DNS:oci-signature-verification-server,DNS:oci-signature-verification-server.oci-image-verification,DNS:oci-signature-verification-server.oci-image-verification.svc")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("error generating TLS key pair with OpenSSL: %v", err)
	}

	if _, err := os.Stat(tlsCertPath(nt)); os.IsNotExist(err) {
		return fmt.Errorf("tls.crt file was not generated")
	}
	if _, err := os.Stat(tlsKeyPath(nt)); os.IsNotExist(err) {
		return fmt.Errorf("tls.key file was not generated")
	}

	nt.T.Log("TLS key pair generated.")

	return nil
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

	if err := createSecret(nt, configmanagement.ControllerNamespace, RootAuthSecretName,
		fmt.Sprintf("ssh=%s", privateKeyPath(nt))); err != nil {
		return "", err
	}

	return privateKeyPath(nt), nil
}

// CreateNamespaceSecrets creates secrets in a given namespace using local paths.
// It skips creating the Secret if the GitProvider is CSR because CSR uses either
// 'gcenode' or 'gcpserviceaccount' for authentication, which doesn't require a Secret.
func CreateNamespaceSecrets(nt *NT, ns string) error {
	if nt.GitProvider.Type() != e2e.CSR {
		privateKeypath := nt.gitPrivateKeyPath
		if len(privateKeypath) == 0 {
			privateKeypath = privateKeyPath(nt)
		}
		nt.T.Logf("Creating Secret %s for Namespace %s", NamespaceAuthSecretName, ns)
		if err := createSecret(nt, ns, NamespaceAuthSecretName, fmt.Sprintf("ssh=%s", privateKeypath)); err != nil {
			return err
		}
	}
	if nt.GitProvider.Type() == e2e.Local {
		caCertPathVal := nt.gitCACertPath
		if len(caCertPathVal) == 0 {
			caCertPathVal = caCertPath(nt, GitSyncSource)
		}
		if err := createSecret(nt, ns, PublicCertSecretName(GitSyncSource), fmt.Sprintf("cert=%s", caCertPathVal)); err != nil {
			return err
		}
	}
	if nt.OCIProvider.Type() == e2e.Local || nt.HelmProvider.Type() == e2e.Local {
		caCertPathVal := nt.registryCACertPath
		if len(caCertPathVal) == 0 {
			caCertPathVal = caCertPath(nt, RegistrySyncSource)
		}
		if err := createSecret(nt, ns, PublicCertSecretName(RegistrySyncSource), fmt.Sprintf("cert=%s", caCertPathVal)); err != nil {
			return err
		}
	}
	return nil
}
