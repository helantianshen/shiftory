package auth

import "testing"

func TestPasswordHasherHashesAndVerifies(t *testing.T) {
	hasher := NewPasswordHasher(PasswordParams{MemoryKiB: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32})
	hash, err := hasher.Hash("correct horse battery staple")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if hash == "correct horse battery staple" {
		t.Fatal("password was stored in plaintext")
	}
	if ok, err := hasher.Verify(hash, "correct horse battery staple"); err != nil || !ok {
		t.Fatalf("correct password did not verify: ok=%v err=%v", ok, err)
	}
	if ok, err := hasher.Verify(hash, "wrong password"); err != nil || ok {
		t.Fatalf("wrong password verified: ok=%v err=%v", ok, err)
	}
}

func TestPasswordHasherRejectsMalformedHash(t *testing.T) {
	hasher := NewPasswordHasher(DefaultPasswordParams())
	if _, err := hasher.Verify("not-an-argon-hash", "password"); err == nil {
		t.Fatal("expected malformed hash to fail")
	}
}
