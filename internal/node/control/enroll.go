package control

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"time"

	"mirror-server/internal/protocol"
	"mirror-server/internal/requestid"
)

type Enroller struct {
	NodeName           string
	Address            string
	TLSConfig          *tls.Config
	CodeFile           string
	CredentialFile     string
	TokenPublicKeyFile string
	Identity           IdentityStore
}

type Credential struct {
	EnrollmentID string `json:"enrollment_id"`
}

func (e Enroller) RunOnce() error {
	if cred, err := e.readCredential(); err == nil && cred.EnrollmentID != "" {
		return e.collect(cred.EnrollmentID)
	}
	code, err := os.ReadFile(e.CodeFile)
	if err != nil {
		return fmt.Errorf("读取配对码文件失败：%w", err)
	}
	keyPEM, csrPEM, fp, err := createCSR(e.NodeName)
	if err != nil {
		return err
	}
	if err := writePrivate(e.Identity.KeyFile, keyPEM, 0o600); err != nil {
		return err
	}
	return e.submit(string(code), string(csrPEM), fp)
}

func (e Enroller) RunUntilComplete(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if err := e.RunOnce(); err != nil {
			return err
		}
		if _, err := os.Stat(e.Identity.CertFile); err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("节点登记等待审批超时")
		}
		time.Sleep(10 * time.Second)
	}
}

func (e Enroller) submit(code, csrPEM, fp string) error {
	reply, err := e.exchange(protocol.TypeEnrollRequest, protocol.EnrollRequest{
		PairingCode: code, PublicName: e.NodeName, CSRPem: csrPEM,
		PublicKeyFingerprint: fp,
		Capabilities:         []string{"heartbeat.v1", "inventory.report.v1", "pressure.report.v1"},
	})
	if err != nil {
		return err
	}
	switch reply.MessageType {
	case protocol.TypeEnrollPending:
		var pending protocol.EnrollPending
		if err := json.Unmarshal(reply.Payload, &pending); err != nil {
			return err
		}
		return e.writeCredential(Credential{EnrollmentID: pending.EnrollmentID})
	case protocol.TypeEnrollCertificate:
		return e.saveCertificate(reply)
	default:
		return fmt.Errorf("登记响应类型不符合预期")
	}
}

func (e Enroller) collect(enrollmentID string) error {
	reply, err := e.exchange(protocol.TypeEnrollCertificate, protocol.EnrollCertificateRequest{EnrollmentID: enrollmentID})
	if err != nil {
		return err
	}
	if reply.MessageType != protocol.TypeEnrollCertificate {
		return fmt.Errorf("证书尚未可领取")
	}
	return e.saveCertificate(reply)
}

func (e Enroller) exchange(messageType string, payload any) (protocol.Envelope, error) {
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 10 * time.Second}, "tcp", e.Address, e.TLSConfig)
	if err != nil {
		return protocol.Envelope{}, err
	}
	defer conn.Close()
	reqID, _ := requestid.New()
	body, _ := json.Marshal(payload)
	err = protocol.WriteFrame(conn, protocol.Envelope{
		ProtocolVersion: protocol.Version, MessageID: reqID,
		MessageType: messageType, SentAt: time.Now().UTC(),
		RequestID: reqID, Payload: body,
	})
	if err != nil {
		return protocol.Envelope{}, err
	}
	return protocol.ReadFrame(conn, protocol.MaxFrameBytes)
}

func (e Enroller) saveCertificate(reply protocol.Envelope) error {
	var cert protocol.EnrollCertificate
	if err := json.Unmarshal(reply.Payload, &cert); err != nil {
		return err
	}
	if err := e.Identity.Save(cert.NodeID, []byte(cert.CertificatePEM), []byte(cert.CAChainPEM)); err != nil {
		return err
	}
	if cert.DownloadTokenPublicKeyPEM != "" {
		if err := writePrivate(e.TokenPublicKeyFile, []byte(cert.DownloadTokenPublicKeyPEM), 0o644); err != nil {
			return err
		}
	}
	_ = os.Remove(e.CodeFile)
	_ = os.Remove(e.CredentialFile)
	return nil
}

func (e Enroller) readCredential() (Credential, error) {
	data, err := os.ReadFile(e.CredentialFile)
	if err != nil {
		return Credential{}, err
	}
	var c Credential
	return c, json.Unmarshal(data, &c)
}

func (e Enroller) writeCredential(c Credential) error {
	data, _ := json.Marshal(c)
	return writePrivate(e.CredentialFile, data, 0o600)
}

func createCSR(name string) ([]byte, []byte, string, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, "", err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, "", err
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader,
		&x509.CertificateRequest{Subject: pkix.Name{CommonName: name}}, key)
	if err != nil {
		return nil, nil, "", err
	}
	pubDER, _ := x509.MarshalPKIXPublicKey(&key.PublicKey)
	sum := sha256.Sum256(pubDER)
	return pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}),
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER}),
		"sha256:" + hex.EncodeToString(sum[:]), nil
}
