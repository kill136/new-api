package chatgpt_web

import (
	"crypto/sha3"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/google/uuid"
)

// chatRequirements 是 /backend-api/sentinel/chat-requirements 的返回。
// 实际请求在 transport.go 用 tls-client 发出（绕过 CF 指纹墙）。
type chatRequirements struct {
	Persona     string `json:"persona"`
	Token       string `json:"token"`
	Proofofwork struct {
		Required   bool   `json:"required"`
		Seed       string `json:"seed"`
		Difficulty string `json:"difficulty"`
	} `json:"proofofwork"`
	Turnstile struct {
		Required bool `json:"required"`
	} `json:"turnstile"`
}

// solveProofOfWork 复刻网页端 sentinel PoW：
//
//	hash = sha3_512(seed + base64(json(config)))
//	命中条件：hex(hash)[:len(difficulty)] <= difficulty（字典序）
//	proof token = "gAAAAAB" + base64(json(config))
//
// 实测（见记忆 chatgpt-web-reverse-feasible）：难度约 6 位十六进制，十几次迭代即解；
// 服务端只校验哈希是否达标，不深校验 config 内容，因此 config 用合理占位即可。
func solveProofOfWork(seed, difficulty, userAgent string) string {
	if difficulty == "" {
		difficulty = "000000"
	}
	config := []any{
		3008,              // 0 屏幕尺寸和
		parseTimeString(), // 1 本地时间字符串
		4294705152,        // 2 内存类常量
		0,                 // 3 循环计数器占位（下面被覆盖）
		userAgent,         // 4 UA（与请求头一致）
		"https://cdn.oaistatic.com/_next/static/chunks/main.js", // 5 缓存脚本 URL
		"dpl=" + randomHex(16), // 6 部署标识
		"en-US",                // 7
		"en-US,en",             // 8
		0,                      // 9
		"plugins",              // 10 navigator key
		"location",             // 11 document key
		"scrollX",              // 12 window key
		0,                      // 13 performance.now()
		uuid.NewString(),       // 14 随机 UUID
		"",                     // 15
		8,                      // 16 hardwareConcurrency
		0,                      // 17 时间基准偏移
	}

	diffLen := len(difficulty)
	for i := 0; i < 500000; i++ {
		config[3] = i
		raw, err := common.Marshal(config)
		if err != nil {
			break
		}
		b64 := base64.StdEncoding.EncodeToString(raw)
		h := sha3.New512()
		h.Write([]byte(seed + b64))
		hexStr := hex.EncodeToString(h.Sum(nil))
		if len(hexStr) >= diffLen && hexStr[:diffLen] <= difficulty {
			return "gAAAAAB" + b64
		}
	}
	// 兜底（几乎不会触发）：返回 seed 的 base64
	return "gAAAAAB" + base64.StdEncoding.EncodeToString([]byte(seed))
}

// parseTimeString 形如 "Tue Jun 02 2026 09:21:00 GMT+0000 (Coordinated Universal Time)"。
func parseTimeString() string {
	return time.Now().UTC().Format("Mon Jan 02 2006 15:04:05") + " GMT+0000 (Coordinated Universal Time)"
}

func randomHex(n int) string {
	s := strings.ReplaceAll(uuid.NewString(), "-", "")
	for len(s) < n {
		s += strings.ReplaceAll(uuid.NewString(), "-", "")
	}
	return s[:n]
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
