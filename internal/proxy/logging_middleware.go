package proxy

import (
	"bytes"
	"io"
	"net/http"
	"trae-proxy-go/internal/logger"
)

type loggingResponseWriter struct {
	http.ResponseWriter
	statusCode int
	written    int64
}

func (lrw *loggingResponseWriter) WriteHeader(code int) {
	lrw.statusCode = code
	lrw.ResponseWriter.WriteHeader(code)
}

func (lrw *loggingResponseWriter) Write(b []byte) (int, error) {
	n, err := lrw.ResponseWriter.Write(b)
	lrw.written += int64(n)
	return n, err
}

type bufferedReadCloser struct {
	buffer *bytes.Buffer
}

func (brc *bufferedReadCloser) Read(p []byte) (n int, err error) {
	return brc.buffer.Read(p)
}

func (brc *bufferedReadCloser) Close() error {
	return nil
}

func LoggingMiddleware(logger *logger.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			lrw := &loggingResponseWriter{
				ResponseWriter: w,
				statusCode:     http.StatusOK,
			}

			reqAddr := r.RemoteAddr
			reqPath := r.URL.Path
			reqParams := r.URL.RawQuery
			reqMethod := r.Method

			var reqBody []byte
			if r.Body != nil {
				reqBody, _ = io.ReadAll(r.Body)
				r.Body = &bufferedReadCloser{buffer: bytes.NewBuffer(reqBody)}
			}

			if logger != nil {
				if len(reqBody) > 0 {
					logger.Info("[Request] 地址: %s | 方法: %s | 路径: %s | 参数: %s | Body: %s",
						reqAddr, reqMethod, reqPath, reqParams, string(reqBody))
				} else {
					logger.Info("[Request] 地址: %s | 方法: %s | 路径: %s | 参数: %s",
						reqAddr, reqMethod, reqPath, reqParams)
				}
			}

			next.ServeHTTP(lrw, r)

			if logger != nil {
				logger.Info("[Response] 路径: %s | 状态码: %d | 写入字节: %d",
					reqPath, lrw.statusCode, lrw.written)
			}
		})
	}
}
