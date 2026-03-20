package keymanager

import (
	"bytes"
	"crypto/rsa"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewKeyManager_Generate(t *testing.T) {
	host := "generate-host"
	port := "1"

	km, err := NewKeyManager(host, port)
	if err != nil {
		t.Fatalf("NewKeyManager failed: %v", err)
	}

	if km.privateKey == nil {
		t.Error("privateKey is nil")
	}
	if km.publicKey == nil {
		t.Error("publicKey is nil")
	}

	privPath := filepath.Join(km.keysPath, "private.der")
	pubPath := filepath.Join(km.keysPath, "public.der")

	if _, err := os.Stat(privPath); err != nil {
		t.Errorf("private key file not created at %s: %v", privPath, err)
	}
	if _, err := os.Stat(pubPath); err != nil {
		t.Errorf("public key file not created at %s: %v", pubPath, err)
	}
}

func TestNewKeyManager_Load(t *testing.T) {
	host := "load-host"
	port := "2"

	km1, err := NewKeyManager(host, port)
	if err != nil {
		t.Fatalf("first NewKeyManager failed: %v", err)
	}
	keyID1 := fingerprint(km1.publicKey)

	km2, err := NewKeyManager(host, port)
	if err != nil {
		t.Fatalf("second NewKeyManager failed: %v", err)
	}
	keyID2 := fingerprint(km2.publicKey)

	if keyID1 != keyID2 {
		t.Error("loaded key is different from generated key")
	}
}

func TestNewKeyManager_EmptyPort(t *testing.T) {
	host := "empty-port-host"
	port := ""

	km, err := NewKeyManager(host, port)
	if err != nil {
		t.Fatalf("NewKeyManager with empty port failed: %v", err)
	}

	// Проверяем, что в имени папки вместо пустого порта используется "noport"
	expectedSuffix := "_noport"
	if !strings.HasSuffix(km.keysPath, expectedSuffix) {
		t.Errorf("keysPath %q should end with %q", km.keysPath, expectedSuffix)
	}
}

func TestPublicKeyPEM(t *testing.T) {
	km, err := NewKeyManager("pem-host", "3")
	if err != nil {
		t.Fatalf("NewKeyManager failed: %v", err)
	}

	pem, err := km.PublicKeyPEM()
	if err != nil {
		t.Fatalf("PublicKeyPEM failed: %v", err)
	}
	if !strings.HasPrefix(pem, "-----BEGIN PUBLIC KEY-----") {
		t.Error("PEM does not start with correct header")
	}
	if !strings.Contains(pem, "-----END PUBLIC KEY-----") {
		t.Error("PEM does not contain correct footer")
	}
}

func TestSanitize(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"localhost", "localhost"},
		{"192.168.1.100", "192_168_1_100"},
		{"server-name", "server-name"},
		{"server:1545", "server_1545"},
		{"a.b.c", "a_b_c"},
		{"", ""}, // теперь пустая строка остается пустой
	}
	for _, tt := range tests {
		result := sanitize(tt.input)
		if result != tt.expected {
			t.Errorf("sanitize(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

// TestEncryptDecrypt проверяет, что данные можно зашифровать и расшифровать.
func TestEncryptDecrypt(t *testing.T) {
	host := "test-encrypt"
	port := "4"

	km, err := NewKeyManager(host, port)
	if err != nil {
		t.Fatalf("NewKeyManager failed: %v", err)
	}

	plaintext := []byte(`{"test":"data","value":123}`)

	encrypted, err := km.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	decrypted, err := km.Decrypt(encrypted)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}

	if !bytes.Equal(plaintext, decrypted) {
		t.Errorf("Decrypted data mismatch: got %q, want %q", decrypted, plaintext)
	}
}

// TestEncryptDecryptWithDifferentKeys проверяет, что нельзя расшифровать чужим ключом.
func TestEncryptDecryptWithDifferentKeys(t *testing.T) {
	km1, err := NewKeyManager("host1", "1")
	if err != nil {
		t.Fatal(err)
	}
	km2, err := NewKeyManager("host2", "2")
	if err != nil {
		t.Fatal(err)
	}

	plaintext := []byte("secret")
	encrypted, err := km1.Encrypt(plaintext)
	if err != nil {
		t.Fatal(err)
	}

	// Попытка расшифровать ключом km2 должна провалиться
	_, err = km2.Decrypt(encrypted)
	if err == nil {
		t.Error("Decrypt with wrong key succeeded, expected error")
	}
}

func TestEncryptDecryptWithPublicKey(t *testing.T) {
	// Создаем временный keymanager (ключи будут созданы во временной директории)
	// Используем уникальные host/port, чтобы не конфликтовать с другими тестами.
	km, err := NewKeyManager("test-encrypt-pub", "1")
	if err != nil {
		t.Fatalf("NewKeyManager failed: %v", err)
	}

	// Получаем публичный ключ в формате PEM
	pubPEM, err := km.PublicKeyPEM()
	if err != nil {
		t.Fatalf("PublicKeyPEM failed: %v", err)
	}

	plaintext := []byte(`{"ibase_default":{"login":"test","password":"secret"},"ibases":{}}`)

	// Шифруем с помощью EncryptWithPublicKey
	encrypted, err := EncryptWithPublicKey(pubPEM, plaintext)
	if err != nil {
		t.Fatalf("EncryptWithPublicKey failed: %v", err)
	}

	// Расшифровываем через тот же keymanager
	decrypted, err := km.Decrypt(encrypted)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}

	if string(plaintext) != string(decrypted) {
		t.Errorf("Decrypted data mismatch:\nwant: %s\ngot:  %s", plaintext, decrypted)
	}
}

func TestEncryptDecryptWithExampleFile(t *testing.T) {

	exampleJSON := `{
		"RAS": {
			"login": "ras_login",
			"password": "ras_pass"
		},
		"ibase_default": {
			"login": "default_login",
			"password": "default_pass"
		},
		"ibases": {
			"ibtest": {
				"login": "test_login",
				"password": "test_pass"
			}
		}
	}`
	data := []byte(exampleJSON)

	km, err := NewKeyManager("example-host", "2")
	if err != nil {
		t.Fatalf("NewKeyManager failed: %v", err)
	}

	encrypted, err := km.Encrypt(data)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	decrypted, err := km.Decrypt(encrypted)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}

	if string(data) != string(decrypted) {
		t.Error("Decrypted data does not match original")
	}
}

func fingerprint(pub *rsa.PublicKey) string {
	return fmt.Sprintf("%d-%d", pub.E, pub.N.Int64())
}
