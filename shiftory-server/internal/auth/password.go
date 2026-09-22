// Package auth 提供密码散列、JWT 签发与账户认证服务，刷新令牌状态由仓储持久化
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// PasswordParams 配置 Argon2id 成本，内存以 KiB 计，盐和摘要长度以字节计
type PasswordParams struct {
	MemoryKiB   uint32
	Iterations  uint32
	Parallelism uint8
	SaltLength  uint32
	KeyLength   uint32
}

// DefaultPasswordParams 返回 Argon2id 的默认成本，内存单位为 KiB
func DefaultPasswordParams() PasswordParams {
	return PasswordParams{MemoryKiB: 19 * 1024, Iterations: 2, Parallelism: 1, SaltLength: 16, KeyLength: 32}
}

// PasswordHasher 按配置成本创建密码摘要，并按存储参数验证既有摘要
type PasswordHasher struct{ params PasswordParams }

// NewPasswordHasher 使用指定成本创建密码散列器，参数由调用方提供
func NewPasswordHasher(params PasswordParams) *PasswordHasher { return &PasswordHasher{params: params} }

// Hash 用随机盐生成包含算法版本和成本参数的 Argon2id 密码摘要
func (h *PasswordHasher) Hash(password string) (string, error) {
	salt := make([]byte, h.params.SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, h.params.Iterations, h.params.MemoryKiB, h.params.Parallelism, h.params.KeyLength)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		h.params.MemoryKiB, h.params.Iterations, h.params.Parallelism,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

// Verify 按摘要中保存的参数验证密码，格式错误与密码不匹配分别返回错误和 false
func (h *PasswordHasher) Verify(encoded, password string) (bool, error) {
	// 先解析已保存摘要的算法和成本参数，验证时不使用当前默认成本
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" {
		return false, fmt.Errorf("invalid argon2id hash format")
	}
	var memory, iterations uint32
	var parallelism uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &parallelism); err != nil {
		return false, fmt.Errorf("parse argon2id parameters: %w", err)
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, fmt.Errorf("decode argon2id salt: %w", err)
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(want) == 0 {
		return false, fmt.Errorf("decode argon2id key")
	}
	// 按原盐和成本重算摘要，再以常量时间比较结果
	got := argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}
