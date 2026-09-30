package xmember

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/danew/cdk-recharge-system/internal/db"
)

const sealSettingKey = "x_seal_key"

var (
	sealMu     sync.Mutex
	sealCached []byte
)

// secretKey 用单独保存的随机密钥，不跟 JWT_SECRET 绑在一起。
// 轮换登录密钥不会把已经发出的上游码解不开。
func secretKey() []byte {
	sealMu.Lock()
	defer sealMu.Unlock()
	if sealCached != nil {
		return sealCached
	}
	raw := loadOrCreateSealKey()
	sum := sha256.Sum256([]byte("x-member-seal:" + raw))
	sealCached = sum[:]
	return sealCached
}

func loadOrCreateSealKey() string {
	if db.DB != nil {
		if raw, err := db.GetSetting(sealSettingKey); err == nil && strings.TrimSpace(raw) != "" {
			return strings.TrimSpace(raw)
		}
	}
	if v := strings.TrimSpace(os.Getenv("X_SEAL_KEY")); v != "" {
		return v
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "x-member-local-key"
	}
	raw := hex.EncodeToString(buf)
	if db.DB != nil {
		if err := db.SetSetting(sealSettingKey, raw); err != nil {
			// 写不进去就只用这一进程的密钥，避免静默换成 JWT。
			return raw
		}
	}
	return raw
}

func legacyKey() []byte {
	raw := strings.TrimSpace(os.Getenv("JWT_SECRET"))
	if raw == "" {
		raw = "x-member-local-key"
	}
	sum := sha256.Sum256([]byte("x-member-v1:" + raw))
	return sum[:]
}

func seal(plain string) string {
	if plain == "" {
		return ""
	}
	return sealWith(secretKey(), plain)
}

func openSeal(enc string) string {
	if enc == "" {
		return ""
	}
	if plain, ok := openWith(secretKey(), enc); ok {
		return plain
	}
	// 旧数据曾用 JWT_SECRET 派生。解得开就继续用，新写入已经走独立密钥。
	if plain, ok := openWith(legacyKey(), enc); ok {
		return plain
	}
	return ""
}

func sealWith(key []byte, plain string) string {
	block, err := aes.NewCipher(key)
	if err != nil {
		return ""
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return ""
	}
	buf := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		return ""
	}
	out := gcm.Seal(buf, buf, []byte(plain), nil)
	return hex.EncodeToString(out)
}

func openWith(key []byte, enc string) (string, bool) {
	raw, err := hex.DecodeString(enc)
	if err != nil {
		return "", false
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", false
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil || len(raw) < gcm.NonceSize() {
		return "", false
	}
	nonce, ct := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", false
	}
	return string(plain), true
}
