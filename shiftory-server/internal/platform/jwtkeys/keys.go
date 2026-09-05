package jwtkeys

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func LoadOrCreate(privatePath, publicPath string) (ed25519.PrivateKey, ed25519.PublicKey, error) {
	privateExists := fileExists(privatePath)
	publicExists := fileExists(publicPath)
	if privateExists != publicExists {
		return nil, nil, errors.New("JWT private and public key files must either both exist or both be absent")
	}
	if privateExists {
		privateKey, err := readPrivate(privatePath)
		if err != nil {
			return nil, nil, err
		}
		publicKey, err := readPublic(publicPath)
		if err != nil {
			return nil, nil, err
		}
		derived := privateKey.Public().(ed25519.PublicKey)
		if !derived.Equal(publicKey) {
			return nil, nil, errors.New("JWT public key does not match private key")
		}
		return privateKey, publicKey, nil
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return nil, nil, err
	}
	publicDER, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		return nil, nil, err
	}
	if err := writePrivateFile(privatePath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER}), 0o600); err != nil {
		return nil, nil, err
	}
	if err := writePrivateFile(publicPath, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER}), 0o644); err != nil {
		_ = os.Remove(privatePath)
		return nil, nil, err
	}
	return privateKey, publicKey, nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func readPrivate(path string) (ed25519.PrivateKey, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(content)
	if block == nil {
		return nil, errors.New("invalid JWT private key PEM")
	}
	value, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	key, ok := value.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New("JWT private key is not Ed25519")
	}
	return key, nil
}

func readPublic(path string) (ed25519.PublicKey, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(content)
	if block == nil {
		return nil, errors.New("invalid JWT public key PEM")
	}
	value, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	key, ok := value.(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("JWT public key is not Ed25519")
	}
	return key, nil
}

func writePrivateFile(path string, content []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".jwt-key-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("persist JWT key: %w", err)
	}
	return nil
}
