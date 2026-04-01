package transport

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"log/slog"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// TLSListener listens on TCP with TLS 1.3.
type TLSListener struct {
	addr     string
	certDir  string
	handler  ConnHandler
	ln       net.Listener
	wg       sync.WaitGroup
	done     chan struct{}
}

// NewTLSListener creates a TLS-encrypted TCP listener.
func NewTLSListener(addr, certDir string, handler ConnHandler) *TLSListener {
	return &TLSListener{
		addr:    addr,
		certDir: certDir,
		handler: handler,
		done:    make(chan struct{}),
	}
}

// Start begins accepting TLS connections. Generates self-signed certs if needed.
func (tl *TLSListener) Start() error {
	tlsCfg, err := tl.loadOrGenerateTLS()
	if err != nil {
		return fmt.Errorf("tls setup: %w", err)
	}

	ln, err := tls.Listen("tcp", tl.addr, tlsCfg)
	if err != nil {
		return fmt.Errorf("listen tls %s: %w", tl.addr, err)
	}
	tl.ln = ln

	slog.Info("tls listener started", "addr", tl.addr)

	tl.wg.Add(1)
	go func() {
		defer tl.wg.Done()
		for {
			conn, err := ln.Accept()
			if err != nil {
				select {
				case <-tl.done:
					return
				default:
					slog.Error("tls accept", "error", err)
					continue
				}
			}
			tl.wg.Add(1)
			go func() {
				defer tl.wg.Done()
				defer conn.Close()
				tl.handler(&FramedConn{Conn: conn})
			}()
		}
	}()

	return nil
}

// Stop shuts down the TLS listener.
func (tl *TLSListener) Stop() error {
	close(tl.done)
	if tl.ln != nil {
		tl.ln.Close()
	}
	tl.wg.Wait()
	return nil
}

// CertFingerprint returns the SHA-256 fingerprint of the server certificate.
func (tl *TLSListener) CertFingerprint() ([]byte, error) {
	certPath := filepath.Join(tl.certDir, "server.crt")
	data, err := os.ReadFile(certPath)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("no PEM data")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, err
	}
	return cert.Raw, nil
}

func (tl *TLSListener) loadOrGenerateTLS() (*tls.Config, error) {
	if err := os.MkdirAll(tl.certDir, 0700); err != nil {
		return nil, err
	}

	certPath := filepath.Join(tl.certDir, "server.crt")
	keyPath := filepath.Join(tl.certDir, "server.key")

	// Generate if not exists.
	if _, err := os.Stat(certPath); os.IsNotExist(err) {
		slog.Info("generating self-signed TLS certificate", "dir", tl.certDir)
		if err := generateSelfSigned(certPath, keyPath); err != nil {
			return nil, err
		}
	}

	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("load keypair: %w", err)
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
	}, nil
}

func generateSelfSigned(certPath, keyPath string) error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("generate key: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return fmt.Errorf("generate serial: %w", err)
	}

	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "notbbg-server"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(10 * 365 * 24 * time.Hour), // 10 years
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:     []string{"localhost"},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return fmt.Errorf("create cert: %w", err)
	}

	// Write cert.
	certFile, err := os.OpenFile(certPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer certFile.Close()
	pem.Encode(certFile, &pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	// Write key.
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	keyFile, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer keyFile.Close()
	pem.Encode(keyFile, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	return nil
}
