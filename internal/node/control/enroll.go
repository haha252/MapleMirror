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
	"errors"
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
	WSAddress          string
	TLSConfig          *tls.Config
	CodeFile           string
	CredentialFile     string
	TokenPublicKeyFile string
	Identity           IdentityStore
}

type Credential struct {
	EnrollmentID string `json:"enrollment_id"`
}

var errEnrollmentPending = errors.New("节点登记仍待审批")

func (e Enroller) RunOnce() error {
	if enrollmentID, err := e.Identity.EnrollmentID(e.CredentialFile); err == nil && enrollmentID != "" {
		return e.collect(enrollmentID)
	}
	code, err := e.Identity.PairingCode(e.CodeFile)
	if err != nil {
		return err
	}
	keyPEM, csrPEM, fp, err := createCSR(e.NodeName)
	if err != nil {
		return err
	}
	if err := e.Identity.SavePendingKey(keyPEM); err != nil {
		return err
	}
	return e.submit(code, string(csrPEM), fp)
}

func (e Enroller) RunUntilComplete(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if _, err := e.Identity.NodeID(); err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("节点登记等待审批超时")
		}
		if err := e.RunOnce(); err != nil {
			if errors.Is(err, errEnrollmentPending) {
				time.Sleep(10 * time.Second)
				continue
			}
			return err
		}
		time.Sleep(10 * time.Second)
	}
}

func (e Enroller) submit(code, csrPEM, fp string) error {
	capabilities := []string{"heartbeat.v1", "inventory.report.v1", "pressure.report.v1"}
	if e.WSAddress != "" {
		capabilities = []string{"control.v2", "node.status.v2", "inventory.snapshot.v2", "task.attempt.v2", "swarm.v1"}
	}
	reply, err := e.exchange(protocol.TypeEnrollRequest, protocol.EnrollRequest{
		PairingCode: code, PublicName: e.NodeName, CSRPem: csrPEM,
		PublicKeyFingerprint: fp, Capabilities: capabilities,
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
		return e.Identity.SaveEnrollmentID(pending.EnrollmentID)
	case protocol.TypeEnrollCertificate:
		return e.saveCertificate(reply)
	case protocol.TypeProtocolError:
		if body, ok := parseProtocolError(reply.Payload); ok && body.Message != "" {
			return errors.New(body.Message)
		}
		return errors.New("登记请求被拒绝")
	default:
		return fmt.Errorf("登记响应类型不符合预期")
	}
}

func (e Enroller) collect(enrollmentID string) error {
	reply, err := e.exchange(protocol.TypeEnrollCertificate, protocol.EnrollCertificateRequest{EnrollmentID: enrollmentID})
	if err != nil {
		return err
	}
	switch reply.MessageType {
	case protocol.TypeEnrollCertificate:
		return e.saveCertificate(reply)
	case protocol.TypeProtocolError:
		if errBody, ok := parseProtocolError(reply.Payload); ok {
			if errBody.Code == "ENROLLMENT_PENDING" {
				return errEnrollmentPending
			}
			if errBody.Message != "" {
				return fmt.Errorf("%s", errBody.Message)
			}
		}
		return fmt.Errorf("证书领取失败")
	default:
		return fmt.Errorf("证书尚未可领取")
	}
}

func (e Enroller) exchange(messageType string, payload any) (protocol.Envelope, error) {
	if e.WSAddress != "" {
		return e.exchangeWS(messageType, payload)
	}
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
	if err := e.Identity.SaveWithToken(cert.NodeID, []byte(cert.CertificatePEM),
		[]byte(cert.CAChainPEM), []byte(cert.DownloadTokenPublicKeyPEM)); err != nil {
		return err
	}
	_ = os.Remove(e.CodeFile)
	_ = os.Remove(e.CredentialFile)
	_ = os.Remove(e.TokenPublicKeyFile)
	_ = os.Remove(e.Identity.CertFile)
	_ = os.Remove(e.Identity.KeyFile)
	_ = os.Remove(e.Identity.CAFile)
	return nil
}

type protocolError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func parseProtocolError(payload []byte) (protocolError, bool) {
	var errBody protocolError
	if err := json.Unmarshal(payload, &errBody); err != nil {
		return protocolError{}, false
	}
	return errBody, true
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
