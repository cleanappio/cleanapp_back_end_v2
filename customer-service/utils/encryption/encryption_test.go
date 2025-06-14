package encryption

import (
	"testing"
)

func TestNewEncryptor(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		wantErr bool
	}{
		{
			name:    "valid 32-byte key",
			key:     "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			wantErr: false,
		},
		{
			name:    "invalid key - too short",
			key:     "0123456789abcdef",
			wantErr: true,
		},
		{
			name:    "invalid key - not hex",
			key:     "not-a-hex-string-not-a-hex-string-not-a-hex-string-not-a-hex-str",
			wantErr: true,
		},
		{
			name:    "empty key",
			key:     "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewEncryptor(tt.key)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewEncryptor() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestEncryptDecrypt(t *testing.T) {
	// Create encryptor with valid key
	key := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	enc, err := NewEncryptor(key)
	if err != nil {
		t.Fatalf("Failed to create encryptor: %v", err)
	}

	tests := []struct {
		name      string
		plaintext string
	}{
		{
			name:      "simple text",
			plaintext: "Hello, World!",
		},
		{
			name:      "email address",
			plaintext: "user@example.com",
		},
		{
			name:      "credit card number",
			plaintext: "4111111111111111",
		},
		{
			name:      "unicode text",
			plaintext: "Hello, 世界! 🌍",
		},
		{
			name:      "empty string",
			plaintext: "",
		},
		{
			name:      "long text",
			plaintext: "This is a very long text that should be encrypted and decrypted correctly without any issues whatsoever.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Encrypt
			ciphertext, err := enc.Encrypt(tt.plaintext)
			if err != nil {
				t.Fatalf("Encrypt() error = %v", err)
			}

			// Verify ciphertext is different from plaintext (unless empty)
			if tt.plaintext != "" && ciphertext == tt.plaintext {
				t.Error("Ciphertext should be different from plaintext")
			}

			// Decrypt
			decrypted, err := enc.Decrypt(ciphertext)
			if err != nil {
				t.Fatalf("Decrypt() error = %v", err)
			}

			// Verify decrypted text matches original
			if decrypted != tt.plaintext {
				t.Errorf("Decrypt() = %v, want %v", decrypted, tt.plaintext)
			}
		})
	}
}

func TestMaskCardNumber(t *testing.T) {
	tests := []struct {
		name       string
		cardNumber string
		want       string
	}{
		{
			name:       "standard card number",
			cardNumber: "4111111111111111",
			want:       "1111",
		},
		{
			name:       "short number",
			cardNumber: "123",
			want:       "****",
		},
		{
			name:       "empty string",
			cardNumber: "",
			want:       "****",
		},
		{
			name:       "exactly 4 digits",
			cardNumber: "1234",
			want:       "1234",
		},
		{
			name:       "5 digits",
			cardNumber: "12345",
			want:       "2345",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MaskCardNumber(tt.cardNumber); got != tt.want {
				t.Errorf("MaskCardNumber() = %v, want %v", got, tt.want)
			}
		})
	}
}
