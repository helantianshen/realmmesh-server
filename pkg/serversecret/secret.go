// Package serversecret 生成和校验中心签发的服务器随机密钥
package serversecret

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
)

const secretBytes = 32

// Generate 生成具有 256 位随机性的密钥，并返回用于持久化的 SHA-256 摘要
// 原始密钥只交付给调用者，数据库保存摘要，后续认证必须提交原始密钥
func Generate() (plain, digest string) {
	random := make([]byte, secretBytes)

	// Read 使用操作系统提供的密码学随机源填满 32 字节；当前 Go 版本在随机源失败时终止进程
	rand.Read(random)

	// RawURLEncoding 将随机字节编码为不带填充的 43 字符文本，便于写入配置和 metadata
	plain = base64.RawURLEncoding.EncodeToString(random)

	// SHA-256 摘要覆盖交付的完整文本，十六进制编码只用于数据库存储
	sum := sha256.Sum256([]byte(plain))
	return plain, hex.EncodeToString(sum[:])
}

// Verify 先检查原始密钥的规范编码，再以常量时间比较摘要
// 固定格式拒绝弱口令和过长输入，数据库摘要本身不能作为登录凭证
func Verify(plain, digest string) bool {
	if len(plain) != 43 || len(digest) != sha256.Size*2 {
		return false
	}

	// Strict 拒绝非零尾部填充位，再次编码确保输入没有被解码器忽略的换行字符
	raw, err := base64.RawURLEncoding.Strict().DecodeString(plain)
	if err != nil || len(raw) != secretBytes || base64.RawURLEncoding.EncodeToString(raw) != plain {
		return false
	}

	// DecodeString 将数据库中的十六进制摘要还原为 32 字节，便于等长比较
	expected, err := hex.DecodeString(digest)
	if err != nil {
		return false
	}

	// 使用同一 SHA-256 算法重算收到的原始密钥，不能直接把传入值当成摘要
	actual := sha256.Sum256([]byte(plain))

	// 等长摘要采用常量时间比较，不通过逐字节提前返回暴露匹配位置
	return subtle.ConstantTimeCompare(actual[:], expected) == 1
}
