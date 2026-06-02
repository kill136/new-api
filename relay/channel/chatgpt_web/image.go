package chatgpt_web

import (
	"bufio"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

// 图像生成（暴露为独立模型 gpt-image-2，走 OpenAI /v1/images/generations）。
//
// 关键事实（实测，见记忆 chatgpt-web-channel-impl）：网页图像生成是 conversation 里的工具调用，
// 且【异步】——首个 SSE 只回 "Processing image" 占位 + conversation_id；真正的图要轮询会话拿到
// asset_pointer（sediment://file_xxx 或 file-service://file_xxx），再 /backend-api/files/{id}/download
// 换签名 URL，最后下载字节。另：临时对话(history_and_training_disabled=true)会禁用图像，故图像请求必须开历史。

const (
	imagePollInterval = 3 * time.Second
	imagePollMaxTries = 40 // 约 120s 上限
)

// buildImageConversationRequest 构造触发图像工具的 conversation 体（开历史）。
func buildImageConversationRequest(prompt string) *conversationRequest {
	m := dto.Message{Role: "user"}
	m.SetStringContent("Generate an image: " + prompt)
	body := buildConversationRequest([]dto.Message{m}, "auto")
	body.HistoryAndTrainingDisabled = false // 临时对话禁用图像生成，这里必须开历史
	return body
}

type fileDownloadResp struct {
	Status      string `json:"status"`
	DownloadURL string `json:"download_url"`
	FileName    string `json:"file_name"`
}

// ImageHandler 在 DoResponse 中被调用：读 conv_id -> 轮询资产 -> 下载 -> 返回 OpenAI 图像响应。
func ImageHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (any, *types.NewAPIError) {
	key, err := ParseWebKey(info.ApiKey)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeInvalidRequest)
	}

	convID := readConversationID(resp)
	if convID == "" {
		return nil, types.NewError(errors.New("chatgpt-web: 未获取到 conversation_id（图像工具可能未触发）"), types.ErrorCodeBadResponseBody)
	}

	assetPointer, err := pollImageAsset(info, key, convID)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}

	dlURL, err := resolveDownloadURL(info, key, assetPointer)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}

	b64, err := fetchImageBase64(info, key, dlURL)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}

	out := dto.ImageResponse{
		Created: common.GetTimestamp(),
		Data:    []dto.ImageData{{B64Json: b64}},
	}
	c.JSON(http.StatusOK, out)
	return &dto.Usage{PromptTokens: 1, CompletionTokens: 0, TotalTokens: 1}, nil
}

// readConversationID 读取首个 conversation SSE 流，提取 conversation_id 后关闭。
func readConversationID(resp *http.Response) string {
	defer service.CloseResponseBodyGracefully(resp)
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := line[len("data:"):]
		if id := jsonStringField(data, "conversation_id"); id != "" {
			return id
		}
	}
	return ""
}

// pollImageAsset 轮询会话直到出现图像资产指针。
func pollImageAsset(info *relaycommon.RelayInfo, key *WebKey, convID string) (string, error) {
	url := strings.TrimRight(info.ChannelBaseUrl, "/") + "/backend-api/conversation/" + convID
	for i := 0; i < imagePollMaxTries; i++ {
		time.Sleep(imagePollInterval)
		gr, err := tlsAuthedGet(info, key, url)
		if err != nil {
			continue
		}
		body, _ := io.ReadAll(gr.Body)
		gr.Body.Close()
		if gr.StatusCode != http.StatusOK {
			continue
		}
		if ptr := findAssetPointer(string(body)); ptr != "" {
			return ptr, nil
		}
	}
	return "", fmt.Errorf("chatgpt-web: 图像在 %ds 内未就绪（轮询超时）", int(imagePollInterval.Seconds())*imagePollMaxTries)
}

// findAssetPointer 在会话 JSON 里找图像资产指针（sediment:// 或 file-service://）。
func findAssetPointer(s string) string {
	for _, scheme := range []string{"sediment://", "file-service://"} {
		if p := strings.Index(s, scheme); p >= 0 {
			rest := s[p:]
			if q := strings.IndexAny(rest, "\"?\\ "); q >= 0 {
				return rest[:q]
			}
			return rest
		}
	}
	return ""
}

// resolveDownloadURL 用 asset pointer 的 file id 换签名下载地址。
func resolveDownloadURL(info *relaycommon.RelayInfo, key *WebKey, assetPointer string) (string, error) {
	fileID := assetPointer
	if i := strings.LastIndex(fileID, "/"); i >= 0 {
		fileID = fileID[i+1:]
	}
	url := strings.TrimRight(info.ChannelBaseUrl, "/") + "/backend-api/files/" + fileID + "/download"
	gr, err := tlsAuthedGet(info, key, url)
	if err != nil {
		return "", err
	}
	defer gr.Body.Close()
	body, _ := io.ReadAll(gr.Body)
	if gr.StatusCode != http.StatusOK {
		return "", fmt.Errorf("chatgpt-web: files/download 状态 %d: %s", gr.StatusCode, truncate(string(body), 200))
	}
	var fd fileDownloadResp
	if err := common.Unmarshal(body, &fd); err != nil {
		return "", fmt.Errorf("chatgpt-web: 解析 files/download 失败: %w", err)
	}
	if fd.DownloadURL == "" {
		return "", fmt.Errorf("chatgpt-web: files/download 无 download_url: %s", truncate(string(body), 200))
	}
	return fd.DownloadURL, nil
}

// fetchImageBase64 下载图片字节并 base64 编码。
func fetchImageBase64(info *relaycommon.RelayInfo, key *WebKey, dlURL string) (string, error) {
	gr, err := tlsAuthedGet(info, key, dlURL)
	if err != nil {
		return "", err
	}
	defer gr.Body.Close()
	body, err := io.ReadAll(gr.Body)
	if err != nil {
		return "", err
	}
	if gr.StatusCode != http.StatusOK {
		return "", fmt.Errorf("chatgpt-web: 下载图片状态 %d", gr.StatusCode)
	}
	if len(body) == 0 {
		return "", errors.New("chatgpt-web: 下载到空图片")
	}
	return base64.StdEncoding.EncodeToString(body), nil
}

// jsonStringField 从一段 JSON 文本里粗略提取 "key": "value" 的 value。
func jsonStringField(s, key string) string {
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
