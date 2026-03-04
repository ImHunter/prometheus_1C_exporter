package crypto

import (
	"testing"

	"crypto-core/keys"
)

func TestFullEncryptionDecryption(t *testing.T) {
	// 1. Генерируем ключи
	keyPair, err := keys.GenerateKeyPair(3072)
	if err != nil {
		t.Fatal("Failed to generate keys:", err)
	}

	// 2. Создаем шифровальщика и расшифровщика
	encryptor := NewEncryptor(keyPair.PublicKey, keyPair.KeyID)
	decryptor := NewDecryptor(keyPair.PrivateKey, keyPair.KeyID)

	// 3. Тестовые данные
	originalData := `{
		"DB_PASSWORD": "postgres123",
		"API_KEY": "sk-123456789",
		"REDIS_PASSWORD": "redis456"
	}`

	// 4. Шифруем
	encrypted, err := encryptor.EncryptString(originalData)
	if err != nil {
		t.Fatal("Encryption failed:", err)
	}

	// 5. Проверяем, что алгоритм указан
	if encrypted.Algorithm != "RSA-OAEP-3072+AES-256-GCM" {
		t.Errorf("Wrong algorithm: %s", encrypted.Algorithm)
	}

	// 6. Расшифровываем
	decrypted, err := decryptor.DecryptString(encrypted)
	if err != nil {
		t.Fatal("Decryption failed:", err)
	}

	// 7. Проверяем, что данные совпадают
	if decrypted != originalData {
		t.Errorf("Data mismatch:\nOriginal: %s\nDecrypted: %s", originalData, decrypted)
	}
}

func TestKeyIDMismatch(t *testing.T) {
	// Генерируем две пары ключей
	keyPair1, _ := keys.GenerateKeyPair(3072)
	keyPair2, _ := keys.GenerateKeyPair(3072)

	encryptor := NewEncryptor(keyPair1.PublicKey, keyPair1.KeyID)
	decryptor := NewDecryptor(keyPair2.PrivateKey, keyPair2.KeyID) // Другой ключ!

	encrypted, err := encryptor.EncryptString("secret data")
	if err != nil {
		t.Fatal(err)
	}

	// Должно упасть с ошибкой о несовпадении KeyID
	_, err = decryptor.DecryptString(encrypted)
	if err == nil {
		t.Error("Expected error due to key ID mismatch, got nil")
	}
}

func TestJSONSerialization(t *testing.T) {
	keyPair, _ := keys.GenerateKeyPair(3072)
	encryptor := NewEncryptor(keyPair.PublicKey, keyPair.KeyID)

	original, err := encryptor.EncryptString("test data")
	if err != nil {
		t.Fatal(err)
	}

	// Сериализуем в JSON
	jsonData, err := original.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}

	// Десериализуем обратно
	var decoded EncryptedPackage
	if err := decoded.UnmarshalJSON(jsonData); err != nil {
		t.Fatal(err)
	}

	// Проверяем поля
	if decoded.KeyID != original.KeyID {
		t.Errorf("KeyID mismatch: %s vs %s", decoded.KeyID, original.KeyID)
	}
	if decoded.EncryptedKey != original.EncryptedKey {
		t.Error("EncryptedKey mismatch")
	}
}

func TestInvalidKeySize(t *testing.T) {
	// Пытаемся создать ключ太小ого размера
	_, err := keys.GenerateKeyPair(1024) // Слишком маленький
	if err == nil {
		t.Error("Expected error for 1024-bit key, got nil")
	}
}

func TestTamperedData(t *testing.T) {
	keyPair, _ := keys.GenerateKeyPair(3072)
	encryptor := NewEncryptor(keyPair.PublicKey, keyPair.KeyID)
	decryptor := NewDecryptor(keyPair.PrivateKey, keyPair.KeyID)

	encrypted, err := encryptor.EncryptString("sensitive data")
	if err != nil {
		t.Fatal(err)
	}

	// Изменяем зашифрованные данные (симулируем атаку)
	tampered := *encrypted
	tampered.EncryptedData = "AAAA" + tampered.EncryptedData[4:]

	// Должно упасть при расшифровке (GCM обнаружит подделку)
	_, err = decryptor.DecryptString(&tampered)
	if err == nil {
		t.Error("Expected error due to tampered data, got nil")
	}
}

func BenchmarkEncryption(b *testing.B) {
	keyPair, _ := keys.GenerateKeyPair(3072)
	encryptor := NewEncryptor(keyPair.PublicKey, keyPair.KeyID)
	data := []byte(`{"test":"data","with":"some fields"}`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		encryptor.Encrypt(data)
	}
}

func BenchmarkDecryption(b *testing.B) {
	keyPair, _ := keys.GenerateKeyPair(3072)
	encryptor := NewEncryptor(keyPair.PublicKey, keyPair.KeyID)
	decryptor := NewDecryptor(keyPair.PrivateKey, keyPair.KeyID)

	data := []byte(`{"test":"data","with":"some fields"}`)
	encrypted, _ := encryptor.Encrypt(data)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		decryptor.Decrypt(encrypted)
	}
}
