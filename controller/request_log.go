package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// GetRequestLogDetail returns the full request/response body for a given request_id (admin)
func GetRequestLogDetail(c *gin.Context) {
	requestId := c.Query("request_id")
	if requestId == "" {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "request_id is required",
		})
		return
	}
	log, err := model.GetRequestLogByRequestId(requestId)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "request log not found",
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    log,
	})
}

// GetUserRequestLogDetail returns the full request/response body for a given request_id (user's own)
func GetUserRequestLogDetail(c *gin.Context) {
	requestId := c.Query("request_id")
	if requestId == "" {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "request_id is required",
		})
		return
	}
	userId := c.GetInt("id")
	log, err := model.GetRequestLogByRequestIdAndUserId(requestId, userId)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "request log not found",
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    log,
	})
}
