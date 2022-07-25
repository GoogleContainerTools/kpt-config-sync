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
	"io/ioutil"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"kpt.dev/configsync/e2e/nomostest/gitproviders"
	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/reconcilermanager/controllers"
)

const gitServerSecret = "ssh-pub"
const namespaceSecret = "ssh-key"
const gitServerCertSecret = "ssl-cert"
const gitServerPublicCertSecret = "git-cert-pub"

func sshDir(nt *NT) string {
	nt.T.Helper()
	return filepath.Join(nt.TmpDir, "ssh")
}

func sslDir(nt *NT) string {
	nt.T.Helper()
	return filepath.Join(nt.TmpDir, "ssl")
}

func privateKeyPath(nt *NT) string {
	nt.T.Helper()
	return filepath.Join(sshDir(nt), "id_rsa.nomos")
}

func publicKeyPath(nt *NT) string {
	nt.T.Helper()
	return filepath.Join(sshDir(nt), "id_rsa.nomos.pub")
}

func caCertPath(nt *NT) string {
	nt.T.Helper()
	return filepath.Join(sslDir(nt), "ca_cert.pem")
}

func certPath(nt *NT) string {
	nt.T.Helper()
	return filepath.Join(sslDir(nt), "cert.pem")
}

func certPrivateKeyPath(nt *NT) string {
	nt.T.Helper()
	return filepath.Join(sslDir(nt), "key.pem")
}

// createSSHKeySecret generates a public/public key pair for the test.
func createSSHKeyPair(nt *NT) {
	err := os.MkdirAll(sshDir(nt), fileMode)
	if err != nil {
		nt.T.Fatal("creating ssh directory:", err)
	}

	// ssh-keygen -t rsa -b 4096 -N "" \
	//   -f /opt/testing/nomos/id_rsa.nomos
	//   -C "key generated for use in e2e tests"
	out, err := exec.Command("ssh-keygen", "-t", "rsa", "-b", "4096", "-N", "",
		"-f", privateKeyPath(nt),
		"-C", "key generated for use in e2e tests").Output()
	if err != nil {
		nt.T.Log(string(out))
		nt.T.Fatal("generating rsa key for ssh:", err)
	}
}

func writePEMToFile(nt *NT, path, pemType string, data []byte) {
	pemBuffer := new(bytes.Buffer)
	err := pem.Encode(pemBuffer, &pem.Block{
		Type:  pemType,
		Bytes: data,
	})
	if err != nil {
		nt.T.Fatal("encoding pem: ", err)
	}
	err = os.WriteFile(path, pemBuffer.Bytes(), 0644)
	if err != nil {
		nt.T.Fatal("writing pem file: ", path, err)
	}
}

func createPrivateCA(nt *NT) {
	err := os.MkdirAll(sslDir(nt), fileMode)
	if err != nil {
		nt.T.Fatal("creating ssl directory:", err)
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
		nt.T.Fatal("creating ca private key:", err)
	}
	caBytes, err := x509.CreateCertificate(rand.Reader, ca, ca, &caPrivateKey.PublicKey, caPrivateKey)
	if err != nil {
		nt.T.Fatal("creating ca cert:", err)
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
		nt.T.Fatal("creating server private key:", err)
	}
	certBytes, err := x509.CreateCertificate(rand.Reader, cert, ca, &certPrivateKey.PublicKey, caPrivateKey)
	if err != nil {
		nt.T.Fatal("creating server cert:", err)
	}

	writePEMToFile(nt, caCertPath(nt), "CERTIFICATE", caBytes)
	writePEMToFile(nt, certPath(nt), "CERTIFICATE", certBytes)
	writePEMToFile(nt, certPrivateKeyPath(nt), "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(certPrivateKey))
}

// createSecret creates secret in the given namespace using 'keypath'.
func createSecret(nt *NT, namespace, name string, keyPaths ...string) {
	args := []string{
		"create", "secret", "generic", name, "-n", namespace,
	}
	for _, kp := range keyPaths {
		args = append(args, "--from-file", kp)
	}
	if err := nt.Get(name, namespace, &corev1.Secret{}); apierrors.IsNotFound(err) {
		nt.MustKubectl(args...)
	}
}

// generateSSHKeys generates a public/public key pair for the test.
//
// It turns out kubectl create secret is annoying to emulate, and it doesn't
// expose the inner logic to outside consumers. So instead of trying to do it
// ourselves, we're shelling out to kubectl to ensure we create a valid set of
// secrets.
func generateSSHKeys(nt *NT) string {
	nt.T.Helper()

	createSSHKeyPair(nt)

	createSecret(nt, configmanagement.ControllerNamespace, controllers.GitCredentialVolume,
		fmt.Sprintf("ssh=%s", privateKeyPath(nt)))

	createSecret(nt, testGitNamespace, gitServerSecret,
		filepath.Join(publicKeyPath(nt)))

	return privateKeyPath(nt)
}

// generateSSLKeys generates a self signed certificate for the test
//
// It turns out kubectl create secret is annoying to emulate, and it doesn't
// expose the inner logic to outside consumers. So instead of trying to do it
// ourselves, we're shelling out to kubectl to ensure we create a valid set of
// secrets.
func generateSSLKeys(nt *NT) string {
	nt.T.Helper()

	createPrivateCA(nt)

	createSecret(nt, configmanagement.ControllerNamespace, gitServerPublicCertSecret,
		fmt.Sprintf("cert=%s", caCertPath(nt)))

	createSecret(nt, testGitNamespace, gitServerCertSecret,
		fmt.Sprintf("server.crt=%s", certPath(nt)),
		fmt.Sprintf("server.key=%s", certPrivateKeyPath(nt)))

	return certPath(nt)
}

// downloadSSHKey downloads the private SSH key from Cloud Secret Manager.
func downloadSSHKey(nt *NT) string {
	dir := sshDir(nt)
	err := os.MkdirAll(dir, fileMode)
	if err != nil {
		nt.T.Fatal("creating ssh directory:", err)
	}

	out, err := gitproviders.FetchCloudSecret(gitproviders.PrivateSSHKey)
	if err != nil {
		nt.T.Log(out)
		nt.T.Fatal("downloading SSH key:", err)
	}

	if err := ioutil.WriteFile(privateKeyPath(nt), []byte(out), 0600); err != nil {
		nt.T.Fatal("saving SSH key:", err)
	}

	createSecret(nt, configmanagement.ControllerNamespace, controllers.GitCredentialVolume,
		fmt.Sprintf("ssh=%s", privateKeyPath(nt)))

	return privateKeyPath(nt)
}

// CreateNamespaceSecret creates secret in a given namespace using privateKeyPath.
func CreateNamespaceSecret(nt *NT, ns string) {
	nt.T.Helper()
	privateKeypath := nt.gitPrivateKeyPath
	if len(privateKeypath) == 0 {
		privateKeypath = privateKeyPath(nt)
	}
	createSecret(nt, ns, namespaceSecret, fmt.Sprintf("ssh=%s", privateKeypath))
}
