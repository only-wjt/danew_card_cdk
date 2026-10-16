package db

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"log"
	"os"
	"strings"
)

// 客户凭据（ChatGPT session / 邮箱密码）落库前用 AES-GCM 加密，防数据库或备份泄露后被直接冒用。
// 密钥取 SECRET_ENC_KEY，未配置时由 JWT_SECRET 派生；两者都不在数据库里。
// 更换密钥后旧密文无法解开：读取返回空串，等同于没有绑定。
const sealPrefix = "enc:v1:"

func sealKey() []byte {
	material := strings.TrimSpace(os.Getenv("SECRET_ENC_KEY"))
	if material == "" {
		material = strings.TrimSpace(os.Getenv("JWT_SECRET"))
	}
	if material == "" {
		material = "dev-only-insecure-seal-key"
	}
	sum := sha256.Sum256([]byte("cdk-seal-v1|" + material))
	return sum[:]
}

func sealAEAD() (cipher.AEAD, error) {
	block, err := aes.NewCipher(sealKey())
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// SealSecret 加密；空串原样返回，已是密文不重复加密。
func SealSecret(plain string) (string, error) {
	if plain == "" || strings.HasPrefix(plain, sealPrefix) {
		return plain, nil
	}
	aead, err := sealAEAD()
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	out := aead.Seal(nonce, nonce, []byte(plain), nil)
	return sealPrefix + base64.RawStdEncoding.EncodeToString(out), nil
}

var errSealCorrupt = errors.New("sealed value corrupt")

// OpenSecret 解密；兼容迁移前的明文。解不开时返回空串并记日志。
func OpenSecret(stored string) string {
	if !strings.HasPrefix(stored, sealPrefix) {
		return stored
	}
	plain, err := openSealed(stored)
	if err != nil {
		log.Printf("[seal] decrypt failed (key changed?): %v", err)
		return ""
	}
	return plain
}

func openSealed(stored string) (string, error) {
	raw, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(stored, sealPrefix))
	if err != nil {
		return "", err
	}
	aead, err := sealAEAD()
	if err != nil {
		return "", err
	}
	if len(raw) < aead.NonceSize() {
		return "", errSealCorrupt
	}
	plain, err := aead.Open(nil, raw[:aead.NonceSize()], raw[aead.NonceSize():], nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

// migrateSealCDKSessions 把存量明文 session 加密。
func migrateSealCDKSessions() error {
	return sealColumn("cdk_session_bindings", "cdk_code", "session_payload")
}

// sealColumn 逐行加密某表某列中尚未加密的非空值。
func sealColumn(table, keyCol, col string) error {
	if DB == nil {
		return nil
	}
	rows, err := DB.Query(`SELECT ` + keyCol + `, ` + col + ` FROM ` + table +
		` WHERE ` + col + ` IS NOT NULL AND ` + col + ` != '' AND ` + col + ` NOT LIKE 'enc:v1:%'`)
	if err != nil {
		return err
	}
	type pending struct{ key, val any }
	var todo []pending
	for rows.Next() {
		var k any
		var v string
		if err := rows.Scan(&k, &v); err != nil {
			rows.Close()
			return err
		}
		todo = append(todo, pending{k, v})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, p := range todo {
		sealed, err := SealSecret(p.val.(string))
		if err != nil {
			return err
		}
		if _, err := DB.Exec(`UPDATE `+table+` SET `+col+` = ? WHERE `+keyCol+` = ?`, sealed, p.key); err != nil {
			return err
		}
	}
	if len(todo) > 0 {
		log.Printf("✓ 已加密 %s.%s 存量明文 %d 行", table, col, len(todo))
	}
	return nil
}

// migrateSealAdminRechargeCreds 把批量充值明细里存量明文凭据加密。
func migrateSealAdminRechargeCreds() error {
	for _, col := range []string{"gpt_password", "email_password", "session"} {
		if err := sealColumn("admin_recharge_items", "id", col); err != nil {
			return err
		}
	}
	return nil
}
