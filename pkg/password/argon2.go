package password

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	memoryKiB   = 19 * 1024
	iterations  = 2
	parallelism = 1
	saltLength  = 16
	keyLength   = 32
	parameters  = "m=19456,t=2,p=1"
)

// Hash 使用随机盐生成包含算法参数的 Argon2id 密码哈希
func Hash(plain string) (string, error) {
	salt := make([]byte, saltLength)
	// 使用密码学安全随机数填满盐，使相同密码的多次哈希使用不同输入
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("%w: %w", ErrSaltGeneration, err)
	}
	// 将密码和盐交给 Argon2id，按迭代轮数、内存 KiB 数和并行度生成指定字节数的哈希
	key := argon2.IDKey([]byte(plain), salt, iterations, memoryKiB, parallelism, keyLength)
	// 使用不带等号填充的 Base64，将盐和哈希编码为可存储的文本
	encoding := base64.RawStdEncoding
	return "$argon2id$v=19$" + parameters + "$" + encoding.EncodeToString(salt) + "$" + encoding.EncodeToString(key), nil
}

// Verify 校验支持的哈希格式并以常量时间比较密码结果
func Verify(plain, encoded string) error {
	// 按美元符号分隔编码串，依次取得算法、版本、参数、盐和哈希
	parts := strings.Split(encoded, "$")
	// 只接受当前支持的参数，避免损坏的哈希导致无界内存或计算消耗
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" || parts[2] != "v=19" || parts[3] != parameters {
		return ErrHashInvalid
	}
	// 严格检查 Base64 尾部填充位，拒绝不符合编码规范的数据
	encoding := base64.RawStdEncoding.Strict()
	// 将存储的盐还原为字节，用于重算待验证密码的哈希
	salt, err := encoding.DecodeString(parts[4])
	if err != nil || len(salt) != saltLength {
		return ErrHashInvalid
	}
	// 将存储的哈希还原为字节，作为比较目标
	expected, err := encoding.DecodeString(parts[5])
	if err != nil || len(expected) != keyLength {
		return ErrHashInvalid
	}
	// 使用原盐和相同参数计算待验证密码的哈希
	actual := argon2.IDKey([]byte(plain), salt, iterations, memoryKiB, parallelism, keyLength)
	// 对等长哈希执行常量时间比较，避免比较耗时暴露匹配前缀长度
	if subtle.ConstantTimeCompare(actual, expected) != 1 {
		return ErrMismatch
	}
	return nil
}
