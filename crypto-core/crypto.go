package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
)

// EncryptedPackage представляет полностью зашифрованный пакет данных
type EncryptedPackage struct {
	KeyID         string `json:"key_id"`         // ID ключа для расшифровки
	EncryptedKey  string `json:"encrypted_key"`  // AES ключ, зашифрованный RSA (base64)
	EncryptedData string `json:"encrypted_data"` // Данные, зашифрованные AES (base64)
	IV            string `json:"iv"`             // Вектор инициализации (base64)
	Algorithm     string `json:"algorithm"`      // "RSA-OAEP-3072+AES-256-GCM"
}

// Encryptor отвечает за шифрование данных
type Encryptor struct {
	publicKey *rsa.PublicKey
	keyID     string
}

// NewEncryptor создает шифровальщика из публичного ключа
func NewEncryptor(publicKey *rsa.PublicKey, keyID string) *Encryptor {
	return &Encryptor{
		publicKey: publicKey,
		keyID:     keyID,
	}
}

// Encrypt шифрует данные (гибридная схема)
func (e *Encryptor) Encrypt(plaintext []byte) (*EncryptedPackage, error) {
	// 1. Генерируем случайный AES-256 ключ
	aesKey := make([]byte, 32) // 256 бит
	if _, err := io.ReadFull(rand.Reader, aesKey); err != nil {
		return nil, fmt.Errorf("failed to generate AES key: %w", err)
	}

	// 2. Генерируем случайный IV для GCM
	iv := make([]byte, 12) // 96 бит - стандарт для GCM
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return nil, fmt.Errorf("failed to generate IV: %w", err)
	}

	// 3. Шифруем данные AES-256-GCM
	encryptedData, err := e.aesEncrypt(aesKey, iv, plaintext)
	if err != nil {
		return nil, err
	}

	// 4. Шифруем AES ключ RSA-OAEP
	encryptedKey, err := rsa.EncryptOAEP(
		sha256.New(),
		rand.Reader,
		e.publicKey,
		aesKey,
		nil, // Дополнительные данные не используем
	)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt AES key: %w", err)
	}

	// 5. Упаковываем всё в структуру
	return &EncryptedPackage{
		KeyID:         e.keyID,
		EncryptedKey:  base64.StdEncoding.EncodeToString(encryptedKey),
		EncryptedData: base64.StdEncoding.EncodeToString(encryptedData),
		IV:            base64.StdEncoding.EncodeToString(iv),
		Algorithm:     "RSA-OAEP-3072+AES-256-GCM",
	}, nil
}

// aesEncrypt шифрует данные AES-256-GCM
func (e *Encryptor) aesEncrypt(key, iv, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create AES cipher: %w", err)
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	// GCM шифрует и добавляет тег аутентификации
	ciphertext := aesGCM.Seal(nil, iv, plaintext, nil)
	return ciphertext, nil
}

// Decryptor отвечает за расшифровку данных
type Decryptor struct {
	privateKey *rsa.PrivateKey
	keyID      string
}

// NewDecryptor создает расшифровщика из приватного ключа
func NewDecryptor(privateKey *rsa.PrivateKey, keyID string) *Decryptor {
	return &Decryptor{
		privateKey: privateKey,
		keyID:      keyID,
	}
}

// Decrypt расшифровывает пакет данных
func (d *Decryptor) Decrypt(pkg *EncryptedPackage) ([]byte, error) {
	// Проверяем, что пакет предназначен для нашего ключа
	if pkg.KeyID != d.keyID {
		return nil, fmt.Errorf("key ID mismatch: expected %s, got %s", d.keyID, pkg.KeyID)
	}

	// 1. Декодируем компоненты из base64
	encryptedKey, err := base64.StdEncoding.DecodeString(pkg.EncryptedKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decode encrypted key: %w", err)
	}

	encryptedData, err := base64.StdEncoding.DecodeString(pkg.EncryptedData)
	if err != nil {
		return nil, fmt.Errorf("failed to decode encrypted data: %w", err)
	}

	iv, err := base64.StdEncoding.DecodeString(pkg.IV)
	if err != nil {
		return nil, fmt.Errorf("failed to decode IV: %w", err)
	}

	// 2. Расшифровываем AES ключ с помощью RSA
	aesKey, err := rsa.DecryptOAEP(
		sha256.New(),
		rand.Reader,
		d.privateKey,
		encryptedKey,
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt AES key: %w", err)
	}

	// 3. Расшифровываем данные AES-256-GCM
	plaintext, err := d.aesDecrypt(aesKey, iv, encryptedData)
	if err != nil {
		return nil, err
	}

	return plaintext, nil
}

// aesDecrypt расшифровывает данные AES-256-GCM
func (d *Decryptor) aesDecrypt(key, iv, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create AES cipher: %w", err)
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	// GCM расшифровывает и проверяет тег аутентификации
	plaintext, err := aesGCM.Open(nil, iv, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt: %w", err)
	}

	return plaintext, nil
}

// EncryptString удобная функция для шифрования строки
func (e *Encryptor) EncryptString(plaintext string) (*EncryptedPackage, error) {
	return e.Encrypt([]byte(plaintext))
}

// DecryptString удобная функция для расшифровки в строку
func (d *Decryptor) DecryptString(pkg *EncryptedPackage) (string, error) {
	data, err := d.Decrypt(pkg)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// MarshalJSON сериализует пакет в JSON (для отправки по сети)
func (p *EncryptedPackage) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		KeyID         string `json:"key_id"`
		EncryptedKey  string `json:"encrypted_key"`
		EncryptedData string `json:"encrypted_data"`
		IV            string `json:"iv"`
		Algorithm     string `json:"algorithm"`
	}{
		KeyID:         p.KeyID,
		EncryptedKey:  p.EncryptedKey,
		EncryptedData: p.EncryptedData,
		IV:            p.IV,
		Algorithm:     p.Algorithm,
	})
}

// UnmarshalJSON десериализует пакет из JSON
func (p *EncryptedPackage) UnmarshalJSON(data []byte) error {
	var aux struct {
		KeyID         string `json:"key_id"`
		EncryptedKey  string `json:"encrypted_key"`
		EncryptedData string `json:"encrypted_data"`
		IV            string `json:"iv"`
		Algorithm     string `json:"algorithm"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	p.KeyID = aux.KeyID
	p.EncryptedKey = aux.EncryptedKey
	p.EncryptedData = aux.EncryptedData
	p.IV = aux.IV
	p.Algorithm = aux.Algorithm
	return nil
}
