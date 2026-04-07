package middleware

import (
	"bytes"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

const KeyResponseBodyWriter = "key_response_body_writer"
const maxResponseCaptureSize = 4 * 1024 * 1024 // 4MB

// responseBodyWriter wraps gin.ResponseWriter to capture the response body
type responseBodyWriter struct {
	gin.ResponseWriter
	body   *bytes.Buffer
	capped bool
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

// GetCapturedResponseBody returns the captured bytes from the writer stored in context.
func GetCapturedResponseBody(c *gin.Context) []byte {
	if w, exists := c.Get(KeyResponseBodyWriter); exists {
		if rbw, ok := w.(*responseBodyWriter); ok {
			return rbw.body.Bytes()
		}
	}
	return nil
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
		c.Set(KeyResponseBodyWriter, writer)
		c.Next()
	}
}
