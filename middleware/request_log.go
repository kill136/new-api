package middleware

import (
	"bytes"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

const KeyResponseBody = "key_response_body"
const maxResponseCaptureSize = 64 * 1024 // 64KB

// responseBodyWriter wraps gin.ResponseWriter to capture the response body
type responseBodyWriter struct {
	gin.ResponseWriter
	body    *bytes.Buffer
	capped  bool
}

func (w *responseBodyWriter) Write(b []byte) (int, error) {
	if !w.capped {
		remaining := maxResponseCaptureSize - w.body.Len()
		if remaining > 0 {
			if len(b) <= remaining {
				w.body.Write(b)
			} else {
				w.body.Write(b[:remaining])
				w.body.WriteString("\n...[truncated]")
				w.capped = true
			}
		}
	}
	return w.ResponseWriter.Write(b)
}

// RequestLogCapture captures the response body for request logging.
// Only active when LogRequestBodyEnabled is true.
func RequestLogCapture() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !common.LogRequestBodyEnabled {
			c.Next()
			return
		}
		writer := &responseBodyWriter{
			ResponseWriter: c.Writer,
			body:           &bytes.Buffer{},
		}
		c.Writer = writer
		c.Next()
		c.Set(KeyResponseBody, writer.body.Bytes())
	}
}
