package public

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"log/slog"
	"math/big"
	"sync"
	"time"

	"mirror-server/internal/logging"
)

type vdfKeyMaterial struct {
	modulus   *big.Int
	lambda    *big.Int
	encoded   string
	modulusID string
	createdAt time.Time
}

type vdfKeyGenerator func() (*rsa.PrivateKey, error)

type vdfKeyManager struct {
	mu         sync.RWMutex
	currentKey *vdfKeyMaterial
	generate   vdfKeyGenerator
	now        func() time.Time
	interval   time.Duration
	logger     *logging.Logger
	stop       chan struct{}
	done       chan struct{}
	closeOnce  sync.Once
}

func newVDFKeyManager(interval time.Duration, logger *logging.Logger,
	generate vdfKeyGenerator) (*vdfKeyManager, error) {
	return newVDFKeyManagerWithClock(interval, logger, generate, time.Now)
}

func newVDFKeyManagerWithClock(interval time.Duration, logger *logging.Logger,
	generate vdfKeyGenerator, now func() time.Time) (*vdfKeyManager, error) {
	if generate == nil {
		generate = func() (*rsa.PrivateKey, error) { return rsa.GenerateKey(rand.Reader, vdfByteSize*8) }
	}
	if now == nil {
		now = time.Now
	}
	manager := &vdfKeyManager{generate: generate, interval: interval, logger: logger,
		now: now, stop: make(chan struct{}), done: make(chan struct{})}
	key, err := manager.generateMaterial()
	if err != nil {
		return nil, err
	}
	manager.currentKey = key
	go manager.rotateLoop()
	return manager, nil
}

func (m *vdfKeyManager) generateMaterial() (*vdfKeyMaterial, error) {
	key, err := m.generate()
	if err != nil {
		return nil, err
	}
	if key == nil || len(key.Primes) < 2 || key.N == nil || key.N.BitLen() != vdfByteSize*8 {
		return nil, errors.New("VDF RSA 密钥必须是 3072 位两素数模数")
	}
	if err := key.Validate(); err != nil {
		return nil, err
	}
	pMinusOne := new(big.Int).Sub(key.Primes[0], big.NewInt(1))
	qMinusOne := new(big.Int).Sub(key.Primes[1], big.NewInt(1))
	gcd := new(big.Int).GCD(nil, nil, pMinusOne, qMinusOne)
	lambda := new(big.Int).Div(new(big.Int).Mul(pMinusOne, qMinusOne), gcd)
	modulus := new(big.Int).Set(key.N)
	encoded, err := encodeVDFInteger(modulus)
	if err != nil {
		return nil, err
	}
	id, err := vdfModulusID(modulus)
	if err != nil {
		return nil, err
	}
	return &vdfKeyMaterial{modulus: modulus, lambda: lambda, encoded: encoded,
		modulusID: id, createdAt: m.now().UTC()}, nil
}

func (m *vdfKeyManager) current() *vdfKeyMaterial {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.currentKey
}

func (m *vdfKeyManager) rotateLoop() {
	defer close(m.done)
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			candidate, err := m.generateMaterial()
			if err != nil {
				if m.logger != nil {
					current := m.current()
					m.logger.Warn(context.Background(), "VDF 模数轮换失败，保留当前模数",
						slog.String("modulus_id", current.modulusID),
						slog.Duration("modulus_age", m.now().Sub(current.createdAt)), slog.String("error", err.Error()))
				}
				continue
			}
			m.mu.Lock()
			previous := m.currentKey
			m.currentKey = candidate
			m.mu.Unlock()
			if m.logger != nil {
				m.logger.Info(context.Background(), "VDF 模数已轮换",
					slog.String("modulus_id", candidate.modulusID),
					slog.Duration("previous_modulus_age", m.now().Sub(previous.createdAt)))
			}
		case <-m.stop:
			return
		}
	}
}

func (m *vdfKeyManager) Close() {
	if m == nil {
		return
	}
	m.closeOnce.Do(func() { close(m.stop); <-m.done })
}
