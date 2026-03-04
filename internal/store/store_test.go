package store

import (
	"os"
	"path/filepath"
	"testing"
)

func tempStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatalf("New store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestEncryptDecrypt(t *testing.T) {
	s := tempStore(t)
	plain := "sk-test-api-key-1234567890"
	enc, err := s.encrypt(plain)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if enc == plain {
		t.Fatal("encrypted value should differ from plaintext")
	}
	if enc[:4] != "enc:" {
		t.Fatalf("encrypted value should start with 'enc:', got %q", enc[:4])
	}
	dec, err := s.decrypt(enc)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if dec != plain {
		t.Fatalf("expected %q, got %q", plain, dec)
	}
}

func TestDecryptLegacyPlaintext(t *testing.T) {
	s := tempStore(t)
	// Unencrypted legacy values should pass through
	plain := "sk-old-plaintext-key"
	dec, err := s.decrypt(plain)
	if err != nil {
		t.Fatalf("decrypt legacy: %v", err)
	}
	if dec != plain {
		t.Fatalf("expected %q, got %q", plain, dec)
	}
}

func TestSecretSetting(t *testing.T) {
	s := tempStore(t)
	key := "ai_api_key"
	value := "sk-my-secret-key-abcdef"

	if err := s.SetSecretSetting(key, value); err != nil {
		t.Fatalf("SetSecretSetting: %v", err)
	}

	// Verify raw value in DB is encrypted
	raw, err := s.GetSetting(key)
	if err != nil {
		t.Fatalf("GetSetting raw: %v", err)
	}
	if raw == value {
		t.Fatal("raw DB value should be encrypted, not plaintext")
	}

	// Verify decrypted retrieval
	got, err := s.GetSecretSetting(key)
	if err != nil {
		t.Fatalf("GetSecretSetting: %v", err)
	}
	if got != value {
		t.Fatalf("expected %q, got %q", value, got)
	}
}

func TestConnectionEncryption(t *testing.T) {
	s := tempStore(t)
	configJSON := `{"type":"postgres","host":"localhost","password":"secret123"}`

	if err := s.SaveConnection("conn-1", "My Postgres", "postgres", configJSON); err != nil {
		t.Fatalf("SaveConnection: %v", err)
	}

	conns, err := s.ListConnections()
	if err != nil {
		t.Fatalf("ListConnections: %v", err)
	}
	if len(conns) != 1 {
		t.Fatalf("expected 1 connection, got %d", len(conns))
	}
	if conns[0].ConfigJSON != configJSON {
		t.Fatalf("decrypted config mismatch: got %q", conns[0].ConfigJSON)
	}

	// Verify raw DB value is encrypted
	var raw string
	err = s.db.QueryRow("SELECT config_json FROM connections WHERE id = ?", "conn-1").Scan(&raw)
	if err != nil {
		t.Fatalf("raw query: %v", err)
	}
	if raw == configJSON {
		t.Fatal("raw DB config_json should be encrypted")
	}
}

func TestEncryptEmpty(t *testing.T) {
	s := tempStore(t)
	enc, err := s.encrypt("")
	if err != nil {
		t.Fatalf("encrypt empty: %v", err)
	}
	if enc != "" {
		t.Fatalf("expected empty, got %q", enc)
	}
	dec, err := s.decrypt("")
	if err != nil {
		t.Fatalf("decrypt empty: %v", err)
	}
	if dec != "" {
		t.Fatalf("expected empty, got %q", dec)
	}
}

func TestStorePersistence(t *testing.T) {
	dir := t.TempDir()

	// Create store, save a secret, close
	s1, err := New(dir)
	if err != nil {
		t.Fatalf("New s1: %v", err)
	}
	if err := s1.SetSecretSetting("test_key", "test_value"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	s1.Close()

	// Reopen store, verify secret survives
	s2, err := New(dir)
	if err != nil {
		t.Fatalf("New s2: %v", err)
	}
	defer s2.Close()

	got, err := s2.GetSecretSetting("test_key")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "test_value" {
		t.Fatalf("expected 'test_value', got %q", got)
	}
}

// Verify the db file actually exists on disk
func TestStoreCreatesFile(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	dbPath := filepath.Join(dir, "warehouse_ui.db")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatal("database file should exist")
	}
}
