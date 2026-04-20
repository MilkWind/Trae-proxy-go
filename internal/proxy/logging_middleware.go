package proxy

import (
	"bytes"
	"compress/gzip"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"
	"trae-proxy-go/internal/logger"
	"trae-proxy-go/internal/traffic"
	"unicode/utf8"
)

const maxLoggedBodyBytes = 8192

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

func LoggingMiddleware(logger *logger.Logger, trafficStore *traffic.Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
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

			reqBodyLog, reqBodyTruncated := formatBodyForLog(reqBody, r.Header.Get("Content-Type"), r.Header.Get("Content-Encoding"))
			if logger != nil {
				if len(reqBodyLog) > 0 {
					logger.Info("[Request] Remote: %s | Method: %s | Path: %s | Query: %s | Body: %s",
						reqAddr, reqMethod, reqPath, reqParams, reqBodyLog)
				} else {
					logger.Info("[Request] Remote: %s | Method: %s | Path: %s | Query: %s",
						reqAddr, reqMethod, reqPath, reqParams)
				}
			}

			next.ServeHTTP(lrw, r)

			durationMs := time.Since(start).Milliseconds()
			if logger != nil {
				logger.Info("[Response] Path: %s | Status: %d | Bytes: %d | DurationMs: %d",
					reqPath, lrw.statusCode, lrw.written, durationMs)
			}

			if trafficStore != nil {
				requestEntry := trafficStore.Add(traffic.Entry{
					Direction:     "request",
					RemoteAddr:    reqAddr,
					Method:        reqMethod,
					Path:          reqPath,
					Query:         reqParams,
					Body:          reqBodyLog,
					BodyTruncated: reqBodyTruncated,
				})
				trafficStore.Add(traffic.Entry{
					TraceID:      requestEntry.ID,
					Direction:    "response",
					Method:       reqMethod,
					Path:         reqPath,
					StatusCode:   lrw.statusCode,
					BytesWritten: lrw.written,
					DurationMs:   durationMs,
				})
			}
		})
	}
}

func formatBodyForLog(body []byte, contentType string, contentEncoding string) (string, bool) {
	if len(body) == 0 {
		return "", false
	}

	if strings.EqualFold(strings.TrimSpace(contentEncoding), "gzip") {
		unzipped, err := gunzipBody(body)
		if err == nil {
			body = unzipped
		} else {
			preview, truncated := truncateBytes(body, 32)
			return fmt.Sprintf("[binary body: gzip decode failed, bytes=%d, hex=%s]", len(body), hex.EncodeToString(preview)), truncated
		}
	}

	mt, _, _ := mime.ParseMediaType(contentType)
	if isLikelyTextBody(mt, body) {
		if len(body) <= maxLoggedBodyBytes {
			return string(body), false
		}
		return string(body[:maxLoggedBodyBytes]), true
	}

	preview, truncated := truncateBytes(body, 32)
	return fmt.Sprintf("[binary body: content_type=%q, bytes=%d, hex=%s]", mt, len(body), hex.EncodeToString(preview)), truncated
}

func gunzipBody(body []byte) ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(reader)
}

func isLikelyTextBody(mediaType string, body []byte) bool {
	if mediaType == "" {
		return utf8.Valid(body)
	}
	if strings.HasPrefix(mediaType, "text/") {
		return true
	}
	switch mediaType {
	case "application/json", "application/xml", "application/javascript", "application/x-www-form-urlencoded":
		return true
	}
	return utf8.Valid(body)
}

func truncateBytes(b []byte, max int) ([]byte, bool) {
	if len(b) <= max {
		return b, false
	}
	return b[:max], true
}
