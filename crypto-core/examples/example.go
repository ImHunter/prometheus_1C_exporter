package main

import (
	"encoding/json"
	"fmt"
	"log"

	"secret-service/crypto"
	"secret-service/crypto/keys"
)

func main() {
	fmt.Println("=== Криптографическое ядро: пример использования ===\n")

	// 1. Сервис генерирует ключи при старте
	fmt.Println("1. Генерируем RSA ключи (3072 бит)...")
	keyPair, err := keys.GenerateKeyPair(3072)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("   KeyID: %s\n", keyPair.KeyID)
	fmt.Printf("   Создан: %s\n", keyPair.CreatedAt.Format("15:04:05"))

	// 2. Публичный ключ отдаём GitLab (в реальности - через эндпоинт)
	fmt.Println("\n2. Публичный ключ (PEM):")
	pubPEM, _ := keyPair.PublicKeyToPEM()
	fmt.Printf("%s", pubPEM)

	// 3. GitLab шифрует секреты
	fmt.Println("\n3. GitLab шифрует секреты...")
	encryptor := crypto.NewEncryptor(keyPair.PublicKey, keyPair.KeyID)

	secrets := map[string]string{
		"DB_PASSWORD": "postgres123",
		"API_KEY":     "sk-abcdef123456",
		"JWT_SECRET":  "jwt-super-secret-key",
	}

	secretsJSON, _ := json.Marshal(secrets)
	encrypted, err := encryptor.Encrypt(secretsJSON)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("   Зашифровано:\n")
	fmt.Printf("   KeyID: %s\n", encrypted.KeyID)
	fmt.Printf("   Алгоритм: %s\n", encrypted.Algorithm)
	fmt.Printf("   EncryptedKey length: %d\n", len(encrypted.EncryptedKey))
	fmt.Printf("   EncryptedData length: %d\n", len(encrypted.EncryptedData))

	// 4. Сервис получает и расшифровывает
	fmt.Println("\n4. Сервис расшифровывает...")
	decryptor := crypto.NewDecryptor(keyPair.PrivateKey, keyPair.KeyID)

	decryptedJSON, err := decryptor.Decrypt(encrypted)
	if err != nil {
		log.Fatal("Ошибка расшифровки:", err)
	}

	var decryptedSecrets map[string]string
	json.Unmarshal(decryptedJSON, &decryptedSecrets)

	fmt.Printf("   Расшифровано успешно!\n")
	fmt.Printf("   Секреты: %+v\n", decryptedSecrets)

	// 5. Демонстрация защиты от подделки
	fmt.Println("\n5. Проверка целостности (защита от подделки):")

	// Пытаемся изменить один символ в зашифрованных данных
	tampered := *encrypted
	tampered.EncryptedData = "X" + tampered.EncryptedData[1:]

	_, err = decryptor.Decrypt(&tampered)
	if err != nil {
		fmt.Printf("   ✅ Подделка обнаружена: %v\n", err)
	} else {
		fmt.Println("   ❌ Ошибка: подделка не обнаружена!")
	}

	fmt.Println("\n=== Готово! Криптографическое ядро работает ===")
}
