package service

import (
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
)

const maxRequestLogBodySize = 4 * 1024 * 1024 // 4MB per field

func truncateBody(body string) string {
	if len(body) > maxRequestLogBodySize {
		return body[:maxRequestLogBodySize] + "\n...[truncated]"
	}
	return body
}

// SaveRequestLog saves the request and response bodies asynchronously
func SaveRequestLog(c *gin.Context, requestBody string, responseBody string, statusCode int, startTime time.Time) {
	if !common.LogRequestBodyEnabled {
		return
	}

	userId := c.GetInt("id")
	requestId := c.GetString(common.RequestIdKey)
	modelName := c.GetString("original_model")
	tokenName := c.GetString("token_name")
	channelId := c.GetInt("channel_id")
	isStream := c.GetBool("is_stream")
	endpoint := ""
	if c.Request != nil && c.Request.URL != nil {
		endpoint = c.Request.URL.Path
	}
	useTime := int(time.Since(startTime).Seconds())

	reqBody := truncateBody(requestBody)
	respBody := truncateBody(responseBody)

	gopool.Go(func() {
		log := &model.RequestLog{
			UserId:       userId,
			CreatedAt:    common.GetTimestamp(),
			RequestId:    requestId,
			RequestBody:  reqBody,
			ResponseBody: respBody,
			ModelName:    modelName,
			TokenName:    tokenName,
			ChannelId:    channelId,
			Endpoint:     endpoint,
			StatusCode:   statusCode,
			UseTime:      useTime,
			IsStream:     isStream,
		}
		err := model.CreateRequestLog(log)
		if err != nil {
			common.SysError("failed to save request log: " + err.Error())
		}
	})
}
