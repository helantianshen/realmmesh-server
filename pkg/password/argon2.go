// 密码安全基础设施：Argon2id 哈希与校验
// 生成随机盐和带参数的哈希编码，并以相同参数重算后进行常量时间比较

// Package password 提供 Argon2id 密码哈希生成及校验，不存储明文密码
package password

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
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

// Hash 为明文密码生成可存入数据库的 Argon2id 编码串
//
// 先生成随机盐，再使用密码和盐计算哈希，最后将盐与哈希编码为文本
// 返回值同时保存算法、版本和计算参数，校验密码时使用其中的盐重算哈希
// 盐可以与哈希一起保存，明文密码不会包含在返回值中
func Hash(plain string) string {
	// 第一步：生成随机盐，让相同密码的多次哈希使用不同的随机输入
	salt := make([]byte, saltLength)
	// rand.Read 填满随机盐；当前 Go 版本在随机源失败时终止进程，不返回错误
	rand.Read(salt)

	// 第二步：计算密码哈希，参数依次为密码、盐、迭代轮数、内存 KiB 数、并行度和输出字节数
	key := argon2.IDKey(
		[]byte(plain),
		salt,
		iterations,
		memoryKiB,
		parallelism,
		keyLength,
	)

	// 第三步：将二进制盐和哈希转换为不带等号填充的 Base64 文本
	encoding := base64.RawStdEncoding
	encodedSalt := encoding.EncodeToString(salt)
	encodedKey := encoding.EncodeToString(key)

	// 编码串以美元符号分隔各字段，开头的空字段使结果以美元符号起始
	encoded := strings.Join([]string{
		"",
		"argon2id",
		"v=19",
		parameters,
		encodedSalt,
		encodedKey,
	}, "$")

	return encoded
}

// Verify 将待验证密码与数据库中保存的 Argon2id 编码串进行核对
//
// 校验不解密原密码，而是先检查编码格式，再取出原盐和预期哈希，
// 使用待验证密码、原盐及相同参数重新计算哈希，最后比较两份哈希
//
// 编码格式为 $argon2id$v=19$参数$盐$哈希；开头的 $ 会产生一个空字段，
// 因而拆分结果共有六项。只有算法、版本和参数均受支持时才执行哈希计算
// 格式错误返回 ErrHashInvalid，密码不匹配返回 ErrMismatch，匹配返回 nil
func Verify(plain, encoded string) error {
	// 第一步：检查字段数量，再访问各字段，避免格式错误导致索引越界
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 {
		return ErrHashInvalid
	}

	prefix := parts[0]
	algorithm := parts[1]
	version := parts[2]
	storedParameters := parts[3]
	encodedSalt := parts[4]
	encodedKey := parts[5]

	// 第二步：确认编码标识及计算参数受支持，不接受任意内存或计算成本
	if prefix != "" || algorithm != "argon2id" {
		return ErrHashInvalid
	}
	if version != "v=19" {
		return ErrHashInvalid
	}
	if storedParameters != parameters {
		return ErrHashInvalid
	}

	// 第三步：还原盐和预期哈希；Strict 检查 Base64 尾部未使用位是否为零
	encoding := base64.RawStdEncoding.Strict()
	salt, err := encoding.DecodeString(encodedSalt)
	if err != nil || len(salt) != saltLength {
		return ErrHashInvalid
	}

	expectedKey, err := encoding.DecodeString(encodedKey)
	if err != nil || len(expectedKey) != keyLength {
		return ErrHashInvalid
	}

	// 第四步：使用原盐重算哈希，不能生成新盐，否则无法与保存的哈希比较
	actualKey := argon2.IDKey(
		[]byte(plain),
		salt,
		iterations,
		memoryKiB,
		parallelism,
		keyLength,
	)

	// 第五步：对等长哈希执行常量时间比较，避免耗时暴露匹配前缀长度
	if subtle.ConstantTimeCompare(actualKey, expectedKey) != 1 {
		return ErrMismatch
	}

	return nil
}
