package keymanager

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// EncryptedPackage представляет зашифрованные данные для передачи.
type EncryptedPackage struct {
	EncryptedKey []byte `json:"encrypted_key"` // AES-ключ, зашифрованный RSA
	IV           []byte `json:"iv"`            // Вектор для AES-CBC (16 байт)
	Data         []byte `json:"data"`          // Зашифрованные данные
}

// KeyManager управляет RSA ключами для конкретного экземпляра экспортера.
type KeyManager struct {
	mu         sync.RWMutex
	privateKey *rsa.PrivateKey
	publicKey  *rsa.PublicKey
	keysPath   string
	log        Logger
}

// Logger определяет минимальный интерфейс для логирования, используемый в keymanager.
type Logger interface {
	Errorf(format string, args ...interface{})
	Infof(format string, args ...interface{})
	Debugf(format string, args ...interface{})
}

// NewKeyManager создает или загружает ключи для пары host/port.
func NewKeyManager(host, port string, log Logger) (*KeyManager, error) {
	if host == "" {
		return nil, fmt.Errorf("host required")
	}
	exePath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("get executable path: %w", err)
	}
	exeDir := filepath.Dir(exePath)

	portPart := port
	if portPart == "" {
		portPart = "noport"
	}
	safeHost := sanitize(host)
	safePort := sanitize(portPart)
	keysDir := filepath.Join(exeDir, "keys", fmt.Sprintf("%s_%s", safeHost, safePort))
	if err := os.MkdirAll(keysDir, 0700); err != nil {
		return nil, fmt.Errorf("create keys directory: %w", err)
	}

	privPath := filepath.Join(keysDir, "private.der")
	pubPath := filepath.Join(keysDir, "public.der")

	km := &KeyManager{
		keysPath: keysDir,
		log:      log,
	}

	if privBytes, err := os.ReadFile(privPath); err == nil {
		priv, err := x509.ParsePKCS1PrivateKey(privBytes)
		if err == nil {
			km.privateKey = priv
			km.publicKey = &priv.PublicKey
			return km, nil
		}
	}

	priv, err := rsa.GenerateKey(rand.Reader, 3072)
	if err != nil {
		return nil, fmt.Errorf("generate RSA key: %w", err)
	}
	km.privateKey = priv
	km.publicKey = &priv.PublicKey

	privBytes := x509.MarshalPKCS1PrivateKey(priv)
	if err := os.WriteFile(privPath, privBytes, 0600); err != nil {
		return nil, fmt.Errorf("write private key: %w", err)
	}

	pubBytes, err := x509.MarshalPKIXPublicKey(km.publicKey)
	if err != nil {
		return nil, fmt.Errorf("marshal public key: %w", err)
	}
	if err := os.WriteFile(pubPath, pubBytes, 0644); err != nil {
		return nil, fmt.Errorf("write public key: %w", err)
	}

	return km, nil
}

// PublicKeyPEM возвращает публичный ключ в формате PEM.
func (km *KeyManager) PublicKeyPEM() (string, error) {
	km.mu.RLock()
	defer km.mu.RUnlock()

	if km.publicKey == nil {
		return "", fmt.Errorf("public key not available")
	}

	pubBytes, err := x509.MarshalPKIXPublicKey(km.publicKey)
	if err != nil {
		return "", fmt.Errorf("marshal public key: %w", err)
	}
	pemBlock := &pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pubBytes,
	}
	return string(pem.EncodeToMemory(pemBlock)), nil
}

// encryptWithPublicKey выполняет гибридное шифрование (RSA + AES-256-CBC)
// с заданным публичным ключом. Возвращает сериализованный JSON EncryptedPackage.
func encryptWithPublicKey(pub *rsa.PublicKey, plaintext []byte) ([]byte, error) {
	// 1. Генерируем случайный AES-256 ключ
	aesKey := make([]byte, 32)
	if _, err := rand.Read(aesKey); err != nil {
		return nil, fmt.Errorf("generate AES key: %w", err)
	}

	// 2. Генерируем случайный IV для AES-CBC (16 байт)
	iv := make([]byte, aes.BlockSize)
	if _, err := rand.Read(iv); err != nil {
		return nil, fmt.Errorf("generate IV: %w", err)
	}

	// 3. Шифруем данные AES-256-CBC с PKCS#7 padding
	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return nil, fmt.Errorf("create AES cipher: %w", err)
	}
	padded := pkcs7Pad(plaintext, aes.BlockSize)
	ciphertext := make([]byte, len(padded))
	mode := cipher.NewCBCEncrypter(block, iv)
	mode.CryptBlocks(ciphertext, padded)

	// 4. Шифруем AES-ключ RSA
	encryptedKey, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, pub, aesKey, nil)
	if err != nil {
		return nil, fmt.Errorf("encrypt AES key: %w", err)
	}

	// 5. Упаковываем в JSON
	pkg := EncryptedPackage{
		EncryptedKey: encryptedKey,
		IV:           iv,
		Data:         ciphertext,
	}
	return json.Marshal(pkg)
}

// Encrypt использует публичный ключ KeyManager для шифрования данных.
func (km *KeyManager) Encrypt(plaintext []byte) ([]byte, error) {
	km.mu.RLock()
	defer km.mu.RUnlock()

	if km.publicKey == nil {
		return nil, fmt.Errorf("public key not available")
	}
	return encryptWithPublicKey(km.publicKey, plaintext)
}

// EncryptWithPublicKey шифрует данные с использованием публичного ключа в формате PEM.
func EncryptWithPublicKey(pemKey string, plaintext []byte) ([]byte, error) {
	block, _ := pem.Decode([]byte(pemKey))
	if block == nil || block.Type != "PUBLIC KEY" {
		return nil, fmt.Errorf("invalid public key PEM")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse public key: %w", err)
	}
	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("not an RSA public key")
	}
	return encryptWithPublicKey(rsaPub, plaintext)
}

// Decrypt расшифровывает данные, зашифрованные методом Encrypt.
func (km *KeyManager) Decrypt(encryptedData []byte) ([]byte, error) {
	km.mu.RLock()
	defer km.mu.RUnlock()

	if km.privateKey == nil {
		return nil, fmt.Errorf("private key not available")
	}

	var pkg EncryptedPackage
	if err := json.Unmarshal(encryptedData, &pkg); err != nil {
		return nil, fmt.Errorf("unmarshal encrypted package: %w", err)
	}

	aesKey, err := rsa.DecryptOAEP(sha256.New(), rand.Reader, km.privateKey, pkg.EncryptedKey, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt AES key: %w", err)
	}
	// Расшифровываем данные AES-256-CBC
	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return nil, fmt.Errorf("create AES cipher: %w", err)
	}
	if len(pkg.IV) != aes.BlockSize {
		return nil, fmt.Errorf("IV length mismatch")
	}
	mode := cipher.NewCBCDecrypter(block, pkg.IV)
	decrypted := make([]byte, len(pkg.Data))
	mode.CryptBlocks(decrypted, pkg.Data)

	plaintext, err := pkcs7Unpad(decrypted, aes.BlockSize)
	if err != nil {
		return nil, fmt.Errorf("unpad data: %w", err)
	}
	return plaintext, nil
}

// sanitize заменяет недопустимые для имени файла символы на '_'.
func sanitize(s string) string {
	if s == "" {
		return ""
	}
	result := make([]rune, 0, len(s))
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_' {
			result = append(result, r)
		} else {
			result = append(result, '_')
		}
	}
	if len(result) == 0 {
		return "default"
	}
	return string(result)
}

// SaveLastSecrets шифрует и сохраняет последние секреты в файл secrets.enc в директории ключей.
func (km *KeyManager) SaveLastSecrets(plaintext []byte) error {
	if km.keysPath == "" {
		return fmt.Errorf("keys path not set")
	}
	encrypted, err := km.Encrypt(plaintext)
	if err != nil {
		return fmt.Errorf("failed to encrypt secrets: %w", err)
	}
	path := filepath.Join(km.keysPath, "secrets.enc")
	return os.WriteFile(path, encrypted, 0600)
}

// LoadLastSecrets загружает и расшифровывает последние секреты из файла secrets.enc.
// Возвращает расшифрованные байты, готовые к парсингу.
func (km *KeyManager) LoadLastSecrets() ([]byte, error) {
	if km.keysPath == "" {
		return nil, fmt.Errorf("keys path not set")
	}
	path := filepath.Join(km.keysPath, "secrets.enc")
	encrypted, err := os.ReadFile(path)
	if err != nil {
		return nil, err // отсутствие файла – нормальная ситуация
	}
	return km.Decrypt(encrypted)
}

// pkcs7Pad добавляет PKCS#7 padding.
func pkcs7Pad(data []byte, blockSize int) []byte {
	padding := blockSize - len(data)%blockSize
	padtext := bytes.Repeat([]byte{byte(padding)}, padding)
	return append(data, padtext...)
}

// pkcs7Unpad удаляет PKCS#7 padding.
func pkcs7Unpad(data []byte, blockSize int) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("empty data")
	}
	padding := int(data[len(data)-1])
	if padding > blockSize || padding == 0 {
		return nil, errors.New("invalid padding")
	}
	for i := 0; i < padding; i++ {
		if data[len(data)-1-i] != byte(padding) {
			return nil, errors.New("invalid padding")
		}
	}
	return data[:len(data)-padding], nil
}
