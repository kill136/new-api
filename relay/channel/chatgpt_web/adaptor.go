package chatgpt_web

import (
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

// Adaptor 实现 ChatGPT 网页逆向渠道。
// 流程：ConvertOpenAIRequest 造 conversation 体 -> SetupRequestHeader 里 sentinel+PoW 拿令牌
// -> DoRequest 复用 channel.DoApiRequest 发 conversation -> DoResponse 解 v1 SSE 转 OpenAI。
type Adaptor struct {
	// promptTokens 在 ConvertOpenAIRequest 阶段算好，DoResponse 估算 usage 时复用。
	// 适配器实例是每请求 new 的，故可安全持有请求级状态。
	promptTokens int
}

func (a *Adaptor) Init(info *relaycommon.RelayInfo) {}

func (a *Adaptor) GetChannelName() string { return ChannelName }

func (a *Adaptor) GetModelList() []string { return ModelList }

func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	return strings.TrimRight(info.ChannelBaseUrl, "/") + "/backend-api/conversation", nil
}

func (a *Adaptor) ConvertOpenAIRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) (any, error) {
	if request == nil {
		return nil, errors.New("chatgpt-web channel: request is nil")
	}
	a.promptTokens = countPromptTokens(request.Messages, info.UpstreamModelName)
	return buildConversationRequest(request.Messages, info.UpstreamModelName), nil
}

func (a *Adaptor) SetupRequestHeader(c *gin.Context, header *http.Header, info *relaycommon.RelayInfo) error {
	// 本渠道的请求构造（鉴权头 + sentinel + PoW）全部在 DoRequest 里用 tls-client 完成，
	// 以绕过 Cloudflare 对 Go 默认 TLS 指纹的 403 拦截，此处无需处理。
	return nil
}

func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (any, error) {
	// 流式时设置 SSE 响应头（原本由 channel.DoApiRequest 内部完成，绕过后这里自己做）。
	if info.IsStream {
		helper.SetEventStreamHeaders(c)
	}
	return doConversationRequest(info, requestBody)
}

func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage any, err *types.NewAPIError) {
	// 图像生成（/v1/images/generations，模型 gpt-image-2）：异步轮询 + 下载
	if info.RelayMode == relayconstant.RelayModeImagesGenerations {
		return ImageHandler(c, info, resp)
	}
	// Responses API（/v1/responses）：把 conversation SSE 合成为 responses 事件
	if info.RelayMode == relayconstant.RelayModeResponses {
		if info.IsStream {
			return ResponsesStreamHandler(c, info, resp, a.promptTokens)
		}
		return ResponsesHandler(c, info, resp, a.promptTokens)
	}
	// Chat Completions（/v1/chat/completions）
	if info.IsStream {
		return StreamHandler(c, info, resp, a.promptTokens)
	}
	return Handler(c, info, resp, a.promptTokens)
}

// ConvertOpenAIResponsesRequest 把 /v1/responses 请求转成 ChatGPT conversation 体。
func (a *Adaptor) ConvertOpenAIResponsesRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) (any, error) {
	body, msgs := buildResponsesConversationRequest(request, info.UpstreamModelName)
	a.promptTokens = countPromptTokens(msgs, info.UpstreamModelName)
	return body, nil
}

// ───── 不支持的端点 ─────

func (a *Adaptor) ConvertRerankRequest(c *gin.Context, relayMode int, request dto.RerankRequest) (any, error) {
	return nil, errors.New("chatgpt-web channel: rerank not supported")
}

func (a *Adaptor) ConvertEmbeddingRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.EmbeddingRequest) (any, error) {
	return nil, errors.New("chatgpt-web channel: embedding not supported")
}

func (a *Adaptor) ConvertAudioRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.AudioRequest) (io.Reader, error) {
	return nil, errors.New("chatgpt-web channel: audio not supported")
}

func (a *Adaptor) ConvertImageRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.ImageRequest) (any, error) {
	if strings.TrimSpace(request.Prompt) == "" {
		return nil, errors.New("chatgpt-web channel: image prompt is required")
	}
	return buildImageConversationRequest(request.Prompt), nil
}

func (a *Adaptor) ConvertClaudeRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.ClaudeRequest) (any, error) {
	return nil, errors.New("chatgpt-web channel: claude messages not supported")
}

func (a *Adaptor) ConvertGeminiRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeminiChatRequest) (any, error) {
	return nil, errors.New("chatgpt-web channel: gemini not supported")
}
