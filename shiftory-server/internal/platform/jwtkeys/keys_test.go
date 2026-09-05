package jwtkeys

import (
	"bytes"
	"path/filepath"
	"testing"
)

func TestLoadOrCreatePersistsSameEd25519KeyPair(t *testing.T) {
	directory := t.TempDir()
	privatePath := filepath.Join(directory, "private.pem")
	publicPath := filepath.Join(directory, "public.pem")
	privateOne, publicOne, err := LoadOrCreate(privatePath, publicPath)
	if err != nil {
		t.Fatal(err)
	}
	privateTwo, publicTwo, err := LoadOrCreate(privatePath, publicPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(privateOne, privateTwo) || !bytes.Equal(publicOne, publicTwo) {
		t.Fatal("expected persisted JWT keys to remain stable")
	}
}
