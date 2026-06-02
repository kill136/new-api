package chatgpt_web

import (
	"bytes"
	"fmt"
	"io"
	nethttp "net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	fhttp "github.com/bogdanfinn/fhttp"
	tls_client "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
)

// 为什么有这个文件（见记忆 chatgpt-web-reverse-feasible 的更正）：
// Cloudflare 会按 TLS 指纹（JA3/JA4）拦截 Go 默认 net/http（实测同一时刻 curl=200、Go=403）。
// 因此本渠道所有打 chatgpt.com 的请求改走 bogdanfinn/tls-client（模拟 Chrome 指纹），
// 再把结果桥接回标准库 *net/http.Response 供 relay 框架与 DoResponse 使用。

// newTLSClient 构造模拟 Chrome 指纹的客户端。
func newTLSClient(info *relaycommon.RelayInfo) (tls_client.HttpClient, error) {
	opts := []tls_client.HttpClientOption{
		tls_client.WithClientProfile(profiles.Chrome_146),
		tls_client.WithTimeoutSeconds(600),
		tls_client.WithCookieJar(tls_client.NewCookieJar()),
	}
	if info != nil && strings.TrimSpace(info.ChannelSetting.Proxy) != "" {
		opts = append(opts, tls_client.WithProxyUrl(strings.TrimSpace(info.ChannelSetting.Proxy)))
	}
	return tls_client.NewHttpClient(tls_client.NewNoopLogger(), opts...)
}

// buildAuthHeaders 构造 ChatGPT 网页所需的鉴权头集合（各端点通用）。
func buildAuthHeaders(key *WebKey) map[string]string {
	return map[string]string{
		"Authorization":      "Bearer " + key.AccessToken,
		"chatgpt-account-id": key.AccountID,
		"OAI-Device-Id":      key.DeviceID,
		"OAI-Language":       "en-US",
		"User-Agent":         defaultUA,
		"Referer":            "https://chatgpt.com/",
		"Origin":             "https://chatgpt.com",
	}
}

// tlsFetchRequirements 用 tls-client 换取 sentinel token + PoW 种子。
func tlsFetchRequirements(client tls_client.HttpClient, baseURL string, headers map[string]string) (*chatRequirements, error) {
	url := strings.TrimRight(baseURL, "/") + "/backend-api/sentinel/chat-requirements"
	req, err := fhttp.NewRequest("POST", url, bytes.NewBufferString("{}"))
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("chatgpt-web: chat-requirements request failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("chatgpt-web: chat-requirements status %d: %s", resp.StatusCode, truncate(string(body), 256))
	}
	var cr chatRequirements
	if err := common.Unmarshal(body, &cr); err != nil {
		return nil, fmt.Errorf("chatgpt-web: parse chat-requirements failed: %w", err)
	}
	if cr.Token == "" {
		return nil, fmt.Errorf("chatgpt-web: empty chat-requirements token: %s", truncate(string(body), 256))
	}
	return &cr, nil
}

// doConversationRequest 完成 sentinel+PoW+conversation 全流程，返回桥接后的标准库响应。
func doConversationRequest(info *relaycommon.RelayInfo, requestBody io.Reader) (*nethttp.Response, error) {
	key, err := ParseWebKey(info.ApiKey)
	if err != nil {
		return nil, err
	}
	client, err := newTLSClient(info)
	if err != nil {
		return nil, err
	}
	headers := buildAuthHeaders(key)

	cr, err := tlsFetchRequirements(client, info.ChannelBaseUrl, headers)
	if err != nil {
		return nil, err
	}
	proof := ""
	if cr.Proofofwork.Required {
		proof = solveProofOfWork(cr.Proofofwork.Seed, cr.Proofofwork.Difficulty, defaultUA)
	}

	bodyBytes, err := io.ReadAll(requestBody)
	if err != nil {
		return nil, err
	}
	url := strings.TrimRight(info.ChannelBaseUrl, "/") + "/backend-api/conversation"
	req, err := fhttp.NewRequest("POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("OpenAI-Sentinel-Chat-Requirements-Token", cr.Token)
	if proof != "" {
		req.Header.Set("OpenAI-Sentinel-Proof-Token", proof)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("chatgpt-web: conversation request failed: %w", err)
	}
	return bridgeResponse(resp), nil
}

// tlsAuthedGet 用 tls-client 发一个带鉴权的 GET（用于图片资产下载等）。
func tlsAuthedGet(info *relaycommon.RelayInfo, key *WebKey, url string) (*nethttp.Response, error) {
	client, err := newTLSClient(info)
	if err != nil {
		return nil, err
	}
	req, err := fhttp.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range buildAuthHeaders(key) {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	return bridgeResponse(resp), nil
}

// bridgeResponse 把 fhttp.Response 桥接为标准库 *net/http.Response（Body 为同一 io.ReadCloser，零拷贝流式）。
func bridgeResponse(fresp *fhttp.Response) *nethttp.Response {
	h := make(nethttp.Header, len(fresp.Header))
	for k, vs := range fresp.Header {
		cp := make([]string, len(vs))
		copy(cp, vs)
		h[k] = cp
	}
	return &nethttp.Response{
		Status:        fresp.Status,
		StatusCode:    fresp.StatusCode,
		Proto:         fresp.Proto,
		Header:        h,
		Body:          fresp.Body,
		ContentLength: fresp.ContentLength,
	}
}
