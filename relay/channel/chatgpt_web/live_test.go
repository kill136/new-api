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
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
)

// doLiveConversation 复用适配器真实逻辑跑一遍 sentinel+PoW+conversation，返回上游 SSE Response。
// 仅用于 CGPT_LIVE=1 的人工集成测试；token 读自 /tmp/cgpt_tok.txt。
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
	resp := doLiveConversation(t, body)
	answer := scanAssistantText(t, resp)
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
	resp := doLiveConversation(t, body)
	answer := scanAssistantText(t, resp)
	t.Logf("RESPONSES ANSWER: %q", answer)
	if !strings.Contains(answer, "4") {
		t.Fatalf("unexpected answer: %q", answer)
	}
}

// TestLiveImage 探针：让网页生成图片，dump 出 SSE 里图像相关结构（asset_pointer 等）。
func TestLiveImage(t *testing.T) {
	if os.Getenv("CGPT_LIVE") != "1" {
		t.Skip("set CGPT_LIVE=1 to run live test")
	}
	msg := dto.Message{Role: "user"}
	msg.SetStringContent("Generate an image: a simple solid red circle centered on a white background. Just create it.")
	body := buildConversationRequest([]dto.Message{msg}, "auto")
	body.HistoryAndTrainingDisabled = false // 临时对话禁用图像生成，这里打开历史以触发图像工具
	resp := doLiveConversation(t, body)
	defer resp.Body.Close()

	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 256*1024), 8*1024*1024)
	n := 0
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(line[len("data:"):])
		// 只打印图像相关 / 工具相关 / 内容类型相关的事件
		low := strings.ToLower(data)
		if strings.Contains(low, "asset_pointer") || strings.Contains(low, "image") ||
			strings.Contains(low, "multimodal") || strings.Contains(low, "dalle") ||
			strings.Contains(low, "tool") || strings.Contains(low, "file-service") ||
			strings.Contains(low, "content_type") {
			t.Logf("IMG_SSE[%d]: %s", n, truncate(data, 1500))
			n++
		}
		if n > 40 {
			break
		}
	}
	t.Logf("total image-related lines: %d", n)
}

// TestLiveImagePoll 探针：验证图像异步链路——发图请求拿 conversation_id，轮询会话拿 asset_pointer，再走 files/download。
func TestLiveImagePoll(t *testing.T) {
	if os.Getenv("CGPT_LIVE") != "1" {
		t.Skip("set CGPT_LIVE=1 to run live test")
	}
	raw, err := os.ReadFile("/tmp/cgpt_tok.txt")
	if err != nil {
		t.Fatal(err)
	}
	tok := strings.TrimSpace(string(raw))
	key, err := ParseWebKey(tok)
	if err != nil {
		t.Fatal(err)
	}
	msg := dto.Message{Role: "user"}
	msg.SetStringContent("Generate an image: a simple solid red circle on a white background.")
	body := buildConversationRequest([]dto.Message{msg}, "auto")
	body.HistoryAndTrainingDisabled = false
	resp := doLiveConversation(t, body)

	convID := ""
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 256*1024), 8*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := line[len("data:"):]
		if convID == "" {
			if id := extractJSONString(data, "conversation_id"); id != "" {
				convID = id
			}
		}
	}
	resp.Body.Close()
	t.Logf("conversation_id=%s", convID)
	if convID == "" {
		t.Fatal("no conversation_id captured")
	}

	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}
	info.ApiKey = tok
	info.ChannelBaseUrl = "https://chatgpt.com"

	assetPointer := ""
	for attempt := 0; attempt < 20; attempt++ {
		time.Sleep(4 * time.Second)
		gr, err := tlsAuthedGet(info, key, "https://chatgpt.com/backend-api/conversation/"+convID)
		if err != nil {
			t.Logf("poll[%d] err %v", attempt, err)
			continue
		}
		b, _ := io.ReadAll(gr.Body)
		gr.Body.Close()
		if gr.StatusCode != 200 {
			t.Logf("poll[%d] HTTP %d", attempt, gr.StatusCode)
			continue
		}
		s := string(b)
		for _, scheme := range []string{"file-service://", "sediment://"} {
			if p := strings.Index(s, scheme); p >= 0 {
				rest := s[p:]
				if q := strings.IndexAny(rest, "\"?"); q >= 0 {
					assetPointer = rest[:q]
				}
				break
			}
		}
		if assetPointer != "" {
			t.Logf("poll[%d] FOUND assetPointer=%s", attempt, assetPointer)
			break
		}
		t.Logf("poll[%d] HTTP 200, image not ready (len=%d)", attempt, len(s))
	}
	if assetPointer == "" {
		t.Fatal("image not ready after polling")
	}

	fileID := assetPointer
	if i := strings.LastIndex(fileID, "/"); i >= 0 {
		fileID = fileID[i+1:]
	}
	dl, err := tlsAuthedGet(info, key, "https://chatgpt.com/backend-api/files/"+fileID+"/download")
	if err != nil {
		t.Fatalf("download meta err %v", err)
	}
	db, _ := io.ReadAll(dl.Body)
	dl.Body.Close()
	t.Logf("files/%s/download HTTP %d body=%s", fileID, dl.StatusCode, truncate(string(db), 500))
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

// extractJSONString 从一段 JSON 文本里粗略提取 "key": "value" 的 value（探针用）。
func extractJSONString(s, key string) string {
	for _, pat := range []string{"\"" + key + "\": \"", "\"" + key + "\":\""} {
		if i := strings.Index(s, pat); i >= 0 {
			rest := s[i+len(pat):]
			if j := strings.Index(rest, "\""); j >= 0 {
				return rest[:j]
			}
		}
	}
	return ""
}

// TestBuildResponsesRequest 纯单测：验证 responses Input（字符串/数组）解析。
func TestBuildResponsesRequest(t *testing.T) {
	// 字符串 input
	req := dto.OpenAIResponsesRequest{Input: json.RawMessage(`"hello"`)}
	_, msgs := buildResponsesConversationRequest(req, "auto")
	if len(msgs) != 1 || msgs[0].Role != "user" || msgs[0].StringContent() != "hello" {
		t.Fatalf("string input parse failed: %+v", msgs)
	}
	// 数组 input + instructions
	req2 := dto.OpenAIResponsesRequest{
		Instructions: json.RawMessage(`"be brief"`),
		Input:        json.RawMessage(`[{"role":"user","content":[{"type":"input_text","text":"hi"}]}]`),
	}
	_, msgs2 := buildResponsesConversationRequest(req2, "auto")
	if len(msgs2) != 2 || msgs2[0].Role != "system" || msgs2[1].StringContent() != "hi" {
		t.Fatalf("array input parse failed: %+v", msgs2)
	}
}
