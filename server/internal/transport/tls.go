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
	return EnsureTLSCert(tl.certDir)
}

// EnsureTLSCert loads the persisted cert+key from certDir if they
// exist, are unexpired, and cover every SAN entry currently
// discoverable on the host (loopback + all non-link-local interface
// IPs + hostname). If any of those checks fail the cert is
// regenerated; otherwise the on-disk pair is reused across restarts.
//
// This is exported so both the raw-TLS listener (port 9473) and the
// HTTPS gateway (port 9474) can share one cert without ordering
// constraints between them.
func EnsureTLSCert(certDir string) (*tls.Config, error) {
	if err := os.MkdirAll(certDir, 0700); err != nil {
		return nil, err
	}

	certPath := filepath.Join(certDir, "server.crt")
	keyPath := filepath.Join(certDir, "server.key")

	ips, dns := gatherServerSANs()

	needGen := false
	switch {
	case !fileExists(certPath) || !fileExists(keyPath):
		needGen = true
	default:
		if reason, ok := certNeedsRefresh(certPath, ips, dns); !ok {
			slog.Info("regenerating TLS cert", "dir", certDir, "reason", reason)
			needGen = true
		}
	}

	if needGen {
		slog.Info("generating self-signed TLS certificate", "dir", certDir,
			"ips", ipStrings(ips), "dns", dns)
		if err := generateSelfSigned(certPath, keyPath, ips, dns); err != nil {
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

// gatherServerSANs builds the SAN set the self-signed cert should
// cover. Always includes loopback + "localhost" so local tools work;
// adds every non-link-local interface IP so phone/other LAN clients
// can connect to the host's LAN IP without cert errors; adds the
// system hostname for good measure.
func gatherServerSANs() ([]net.IP, []string) {
	ips := []net.IP{
		net.ParseIP("127.0.0.1"),
		net.ParseIP("::1"),
	}
	dns := []string{"localhost"}

	if host, err := os.Hostname(); err == nil && host != "" && host != "localhost" {
		dns = append(dns, host)
	}

	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			ipNet, ok := a.(*net.IPNet)
			if !ok || ipNet.IP == nil {
				continue
			}
			ip := ipNet.IP
			if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsMulticast() {
				continue
			}
			// Keep IPv4 + global IPv6; skip anything unresolved.
			if ip.To4() != nil || ip.To16() != nil {
				ips = append(ips, ip)
			}
		}
	}

	return ips, dns
}

// certNeedsRefresh returns a human-readable reason string and false
// when the on-disk cert should be regenerated: expired, not yet
// valid, or missing any of the required SAN entries.
func certNeedsRefresh(certPath string, wantIPs []net.IP, wantDNS []string) (string, bool) {
	data, err := os.ReadFile(certPath)
	if err != nil {
		return "unreadable", false
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return "malformed PEM", false
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "unparseable", false
	}

	now := time.Now()
	if now.After(cert.NotAfter) {
		return "expired", false
	}
	if now.Before(cert.NotBefore) {
		return "not yet valid", false
	}

	haveIPs := make(map[string]struct{}, len(cert.IPAddresses))
	for _, ip := range cert.IPAddresses {
		haveIPs[ip.String()] = struct{}{}
	}
	for _, ip := range wantIPs {
		if _, ok := haveIPs[ip.String()]; !ok {
			return "missing IP SAN: " + ip.String(), false
		}
	}

	haveDNS := make(map[string]struct{}, len(cert.DNSNames))
	for _, name := range cert.DNSNames {
		haveDNS[name] = struct{}{}
	}
	for _, name := range wantDNS {
		if _, ok := haveDNS[name]; !ok {
			return "missing DNS SAN: " + name, false
		}
	}
	return "", true
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func ipStrings(ips []net.IP) []string {
	out := make([]string, 0, len(ips))
	for _, ip := range ips {
		out = append(out, ip.String())
	}
	return out
}

func generateSelfSigned(certPath, keyPath string, ips []net.IP, dns []string) error {
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
		NotBefore:    time.Now().Add(-1 * time.Hour), // tolerate small clock skew
		NotAfter:     time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  ips,
		DNSNames:     dns,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return fmt.Errorf("create cert: %w", err)
	}

	certFile, err := os.OpenFile(certPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer certFile.Close()
	_ = pem.Encode(certFile, &pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	keyFile, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer keyFile.Close()
	_ = pem.Encode(keyFile, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	return nil
}
