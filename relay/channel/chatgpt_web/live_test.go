package chatgpt_web

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
)

// 这些是人工集成测试，仅当 CGPT_LIVE=1 时运行，token 读自 /tmp/cgpt_tok.txt。
// 走的是适配器的真实生产函数（tls-client 传输 + sentinel + PoW + SSE 解析）。

// doLiveConversation 用生产传输跑一遍 sentinel+PoW+conversation，返回上游 SSE 响应。
func doLiveConversation(t *testing.T, body *conversationRequest) *http.Response {
	t.Helper()
	raw, err := os.ReadFile("/tmp/cgpt_tok.txt")
	if err != nil {
		t.Fatal(err)
	}
	rawBody, _ := common.Marshal(body)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}
	info.ApiKey = strings.TrimSpace(string(raw))
	info.ChannelBaseUrl = "https://chatgpt.com"
	resp, err := doConversationRequest(info, bytes.NewReader(rawBody))
	if err != nil {
		t.Fatalf("doConversationRequest: %v", err)
	}
	t.Logf("conversation HTTP %d", resp.StatusCode)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("status %d body=%s", resp.StatusCode, truncate(string(b), 500))
	}
	return resp
}

// scanAssistantText 用适配器的 deltaState 解析上游 SSE，返回拼出的 assistant 文本。
func scanAssistantText(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	state := &deltaState{}
	var full strings.Builder
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(line[len("data:"):])
		delta, done := state.apply(data)
		if delta != "" {
			full.WriteString(delta)
		}
		if done {
			break
		}
	}
	return full.String()
}

// TestLiveChat 验证 chat/completions 路径：OpenAI messages -> conversation -> SSE -> 文本。
func TestLiveChat(t *testing.T) {
	if os.Getenv("CGPT_LIVE") != "1" {
		t.Skip("set CGPT_LIVE=1 to run live test")
	}
	msg := dto.Message{Role: "user"}
	msg.SetStringContent("3*7=? Reply with only the number.")
	body := buildConversationRequest([]dto.Message{msg}, "auto")
	answer := scanAssistantText(t, doLiveConversation(t, body))
	t.Logf("CHAT ANSWER: %q", answer)
	if !strings.Contains(answer, "21") {
		t.Fatalf("unexpected answer: %q", answer)
	}
}

// TestLiveResponses 验证 Responses 路径：responses Input -> conversation -> SSE -> 文本。
func TestLiveResponses(t *testing.T) {
	if os.Getenv("CGPT_LIVE") != "1" {
		t.Skip("set CGPT_LIVE=1 to run live test")
	}
	req := dto.OpenAIResponsesRequest{
		Model: "auto",
		Input: json.RawMessage(`"2+2=? Reply with only the number."`),
	}
	body, msgs := buildResponsesConversationRequest(req, "auto")
	if len(msgs) == 0 {
		t.Fatal("buildResponsesConversationRequest produced no messages")
	}
	answer := scanAssistantText(t, doLiveConversation(t, body))
	t.Logf("RESPONSES ANSWER: %q", answer)
	if !strings.Contains(answer, "4") {
		t.Fatalf("unexpected answer: %q", answer)
	}
}

// TestLiveImageGen 端到端验证生产 ImageHandler：建图请求体 -> conversation -> 轮询 -> 下载 -> OpenAI 图像响应。
func TestLiveImageGen(t *testing.T) {
	if os.Getenv("CGPT_LIVE") != "1" {
		t.Skip("set CGPT_LIVE=1 to run live test")
	}
	raw, err := os.ReadFile("/tmp/cgpt_tok.txt")
	if err != nil {
		t.Fatal(err)
	}
	tok := strings.TrimSpace(string(raw))

	body := buildImageConversationRequest("a simple solid red circle centered on a white background")
	resp := doLiveConversation(t, body)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)

	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}
	info.ApiKey = tok
	info.ChannelBaseUrl = "https://chatgpt.com"

	usage, apiErr := ImageHandler(c, info, resp)
	if apiErr != nil {
		t.Fatalf("ImageHandler error: %v", apiErr)
	}
	t.Logf("http=%d usage=%+v body_len=%d", w.Code, usage, w.Body.Len())

	var ir dto.ImageResponse
	if err := common.Unmarshal(w.Body.Bytes(), &ir); err != nil {
		t.Fatalf("unmarshal image response: %v", err)
	}
	if len(ir.Data) == 0 || ir.Data[0].B64Json == "" {
		t.Fatalf("no image data in response: %s", truncate(w.Body.String(), 300))
	}
	imgBytes, err := base64.StdEncoding.DecodeString(ir.Data[0].B64Json)
	if err != nil {
		t.Fatalf("b64 decode: %v", err)
	}
	t.Logf("IMAGE OK: %d bytes, head=%x", len(imgBytes), imgBytes[:min(8, len(imgBytes))])
	if len(imgBytes) < 1000 {
		t.Fatalf("image too small: %d bytes", len(imgBytes))
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// TestBuildResponsesRequest 纯单测：验证 responses Input（字符串/数组）解析。
func TestBuildResponsesRequest(t *testing.T) {
	req := dto.OpenAIResponsesRequest{Input: json.RawMessage(`"hello"`)}
	_, msgs := buildResponsesConversationRequest(req, "auto")
	if len(msgs) != 1 || msgs[0].Role != "user" || msgs[0].StringContent() != "hello" {
		t.Fatalf("string input parse failed: %+v", msgs)
	}
	req2 := dto.OpenAIResponsesRequest{
		Instructions: json.RawMessage(`"be brief"`),
		Input:        json.RawMessage(`[{"role":"user","content":[{"type":"input_text","text":"hi"}]}]`),
	}
	_, msgs2 := buildResponsesConversationRequest(req2, "auto")
	if len(msgs2) != 2 || msgs2[0].Role != "system" || msgs2[1].StringContent() != "hi" {
		t.Fatalf("array input parse failed: %+v", msgs2)
	}
}
