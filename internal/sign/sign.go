// Package sign 实现更新清单的 ed25519 签名与校验。
// 整个更新链路只有一个信任根：你自己持有的私钥。
package sign

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"strings"
)

// GenerateKey 返回 base64 编码的 (公钥, 私钥种子)。
func GenerateKey() (pub, seed string, err error) {
	p, s, err := ed25519.GenerateKey(nil)
	if err != nil {
		return "", "", err
	}
	return base64.StdEncoding.EncodeToString(p), base64.StdEncoding.EncodeToString(s.Seed()), nil
}

// Sign 用 base64 私钥种子对 data 签名，返回 base64 签名。
func Sign(seedB64 string, data []byte) (string, error) {
	seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(seedB64))
	if err != nil || len(seed) != ed25519.SeedSize {
		return "", errors.New("私钥格式错误")
	}
	return base64.StdEncoding.EncodeToString(ed25519.Sign(ed25519.NewKeyFromSeed(seed), data)), nil
}

// Verify 校验签名；公钥或签名格式错误、签名不匹配都会返回错误。
func Verify(pubB64 string, data []byte, sigB64 string) error {
	pub, err := base64.StdEncoding.DecodeString(strings.TrimSpace(pubB64))
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return errors.New("内置公钥无效（构建时未注入？）")
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(sigB64))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return errors.New("签名格式错误")
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), data, sig) {
		return errors.New("签名校验失败：清单可能被篡改")
	}
	return nil
}

// FileSHA256 计算文件的十六进制 SHA-256。
func FileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
