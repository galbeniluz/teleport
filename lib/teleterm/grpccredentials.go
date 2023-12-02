/*
 * Teleport
 * Copyright (C) 2023  Gravitational, Inc.
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU Affero General Public License for more details.
 *
 * You should have received a copy of the GNU Affero General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 */

package teleterm

import (
	"crypto/tls"
	"crypto/x509"
	"os"
	"path/filepath"

	"github.com/gravitational/trace"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/gravitational/teleport/api/utils/keys"
	"github.com/gravitational/teleport/lib/utils/cert"
)

const (
	// tshdCertFileName is the file name of the cert created by the tshd process. The Electron app
	// expects it to exist under this name in the certs dir passed through a flag to tshd.
	tshdCertFileName = "tshd.crt"
	// rendererCertFileName is the file name of the cert created by the renderer process of the
	// Electron app.
	rendererCertFileName = "renderer.crt"
)

// createServerCredentials creates mTLS credentials for a gRPC server. The client cert file is read
// only on an incoming connection, not upfront. The way Connect startup is set up guarantees that by
// the time the client reaches out to us, its public key is saved to the file under clientCertPath.
func createServerCredentials(serverKeyPair tls.Certificate, clientCertPath string) (grpc.ServerOption, error) {
	config := &tls.Config{
		ClientAuth:   tls.RequireAndVerifyClientCert,
		Certificates: []tls.Certificate{serverKeyPair},
	}

	config.GetConfigForClient = func(_ *tls.ClientHelloInfo) (*tls.Config, error) {
		clientCert, err := os.ReadFile(clientCertPath)
		if err != nil {
			log.WithError(err).Error("Failed to read the client cert file")
			// Fall back to the default config.
			return nil, nil
		}

		certPool := x509.NewCertPool()
		if !certPool.AppendCertsFromPEM(clientCert) {
			log.Error("Failed to add the client cert to the pool")
			// Fall back to the default config.
			return nil, nil
		}

		configClone := config.Clone()
		configClone.ClientCAs = certPool

		return configClone, nil
	}

	return grpc.Creds(credentials.NewTLS(config)), nil
}

// createClientCredentials creates mTLS credentials for a gRPC client. The server cert file is read
// upfront as there is no way to lazily add RootCAs to a tls.Config.
func createClientCredentials(clientKeyPair tls.Certificate, serverCertPath string) (grpc.DialOption, error) {
	config, err := createClientTLSConfig(clientKeyPair, serverCertPath)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	return grpc.WithTransportCredentials(credentials.NewTLS(config)), nil
}

func createClientTLSConfig(clientKeyPair tls.Certificate, serverCertPath string) (*tls.Config, error) {
	serverCert, err := os.ReadFile(serverCertPath)
	if err != nil {
		return nil, trace.Wrap(err, "failed to read the server cert file")
	}

	certPool := x509.NewCertPool()
	if !certPool.AppendCertsFromPEM(serverCert) {
		return nil, trace.BadParameter("failed to add server cert to pool")
	}

	return &tls.Config{
		Certificates: []tls.Certificate{clientKeyPair},
		RootCAs:      certPool,
	}, nil
}

func generateAndSaveCert(targetPath string, eku ...x509.ExtKeyUsage) (tls.Certificate, error) {
	// The cert is first saved under a temp path and then renamed to targetPath. This prevents other
	// processes from reading a half-written file.
	tempFile, err := os.CreateTemp(filepath.Dir(targetPath), filepath.Base(targetPath))
	if err != nil {
		return tls.Certificate{}, trace.Wrap(err)
	}
	defer os.Remove(tempFile.Name())

	cert, err := cert.GenerateSelfSignedCert([]string{"localhost"}, nil, eku...)
	if err != nil {
		return tls.Certificate{}, trace.Wrap(err, "failed to generate the certificate")
	}

	if err = tempFile.Chmod(0600); err != nil {
		return tls.Certificate{}, trace.Wrap(err)
	}

	if _, err = tempFile.Write(cert.Cert); err != nil {
		return tls.Certificate{}, trace.Wrap(err)
	}

	if err = tempFile.Close(); err != nil {
		return tls.Certificate{}, trace.Wrap(err)
	}

	if err = os.Rename(tempFile.Name(), targetPath); err != nil {
		return tls.Certificate{}, trace.Wrap(err)
	}

	certificate, err := keys.X509KeyPair(cert.Cert, cert.PrivateKey)
	if err != nil {
		return tls.Certificate{}, trace.Wrap(err)
	}

	return certificate, nil
}
