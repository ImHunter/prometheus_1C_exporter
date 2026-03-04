package keys

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"time"
)

// KeyPair представляет пару RSA ключей с метаданными
type KeyPair struct {
	PrivateKey *rsa.PrivateKey
	PublicKey  *rsa.PublicKey
	KeyID      string
	CreatedAt  time.Time
}

// GenerateKeyPair создает новую пару RSA ключей
func GenerateKeyPair(bits int) (*KeyPair, error) {
	if bits < 2048 {
		return nil, fmt.Errorf("RSA key size must be at least 2048 bits, got %d", bits)
	}

	privateKey, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		return nil, fmt.Errorf("failed to generate RSA key: %w", err)
	}

	// Хеш от публичного ключа используем как ID
	pubKeyBytes, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		return nil, err
	}
	keyID := fmt.Sprintf("%x", sha256.Sum256(pubKeyBytes))[:16]

	return &KeyPair{
		PrivateKey: privateKey,
		PublicKey:  &privateKey.PublicKey,
		KeyID:      keyID,
		CreatedAt:  time.Now(),
	}, nil
}

// PublicKeyToPEM конвертирует публичный ключ в PEM формат
func (kp *KeyPair) PublicKeyToPEM() (string, error) {
	pubKeyBytes, err := x509.MarshalPKIXPublicKey(kp.PublicKey)
	if err != nil {
		return "", err
	}

	pemBlock := &pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pubKeyBytes,
	}
	return string(pem.EncodeToMemory(pemBlock)), nil
}

// PrivateKeyToPEM конвертирует приватный ключ в PEM формат (для сохранения)
func (kp *KeyPair) PrivateKeyToPEM() string {
	pemBlock := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(kp.PrivateKey),
	}
	return string(pem.EncodeToMemory(pemBlock))
}

// PublicKeyFromPEM восстанавливает публичный ключ из PEM
func PublicKeyFromPEM(pemStr string) (*rsa.PublicKey, string, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil || block.Type != "PUBLIC KEY" {
		return nil, "", fmt.Errorf("failed to decode PEM block")
	}

	pubKey, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, "", err
	}

	rsaPubKey, ok := pubKey.(*rsa.PublicKey)
	if !ok {
		return nil, "", fmt.Errorf("not an RSA public key")
	}

	// Вычисляем KeyID
	keyID := fmt.Sprintf("%x", sha256.Sum256(block.Bytes))[:16]

	return rsaPubKey, keyID, nil
}
