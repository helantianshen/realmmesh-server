// HTTP 传输基础设施：服务构建与请求边界
// 配置 JSON 请求解析、请求超时与响应缓存策略

// Package server 构建 HTTP 和 gRPC 服务，组装路由、认证边界与请求日志
package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"time"

	khttp "github.com/go-kratos/kratos/v3/transport/http"
	"github.com/helantianshen/realmmesh-server/internal/biz"
	"github.com/helantianshen/realmmesh-server/internal/handler"
)

const (
	httpRequestTimeout      = 10 * time.Second // HTTP 请求上下文超时，限制业务处理的等待时间
	maxJSONBodyBytes        = 4096             // JSON 请求正文的最大字节数
	maxWarehousePolicyBytes = 65536            // 仓库名单配置 JSON 正文的最大字节数
)

// NewHTTPServer 创建 HTTP 传输服务，配置请求超时和 JSON 解码边界
func NewHTTPServer(addr string) *khttp.Server {
	srv := khttp.NewServer(
		khttp.Address(addr),
		khttp.Timeout(httpRequestTimeout),
		khttp.RequestDecoder(decodeJSONRequest),
	)

	return srv
}

// decodeJSONRequest 使用标准库解析单个 JSON 对象，限制请求体以防无界内存占用
// 不接受表单或未知字段，避免客户端误以为注册时可指定管理员权限
func decodeJSONRequest(request *http.Request, target any) error {
	// 1. ParseMediaType 分离媒体类型和 charset 等参数，只允许 JSON 正文
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return handler.ErrInvalidRequest
	}

	// 2. MaxBytesReader 在读取超限时返回错误，不会把完整超长正文载入内存
	limit := int64(maxJSONBodyBytes)
	if request.URL.Path == "/api/v1/admin/warehouse/set-policy" {
		limit = maxWarehousePolicyBytes
	}
	request.Body = http.MaxBytesReader(nil, request.Body, limit)
	decoder := json.NewDecoder(request.Body)

	// 3. DisallowUnknownFields 拒绝请求结构体未声明的字段，Decode 完成字段类型检查
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return handler.ErrInvalidRequest
	}

	// 4. 再次解码必须到达 EOF，确保正文只有一个 JSON 值，可带尾部空白
	var trailingValue any
	if err := decoder.Decode(&trailingValue); err != io.EOF {
		return handler.ErrInvalidRequest
	}

	return nil
}

// noStore 禁止缓存包含令牌或账户信息的成功和失败响应
func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(writer, request)
	})
}

// operationRequest 将 HTTP 来源和关联标识传入业务，并在响应头及运行日志中保留同一标识
// 只记录方法和耗时，不记录请求正文、查询参数或认证头；业务结果由业务层单独审计
func operationRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		started := time.Now()
		id := requestID(request.Header.Values("X-Request-ID"))
		ctx := biz.WithOperationRequest(request.Context(), biz.OperationRequest{Source: "http", RequestID: id})
		writer.Header().Set("X-Request-ID", id)

		next.ServeHTTP(writer, request.WithContext(ctx))
		slog.InfoContext(ctx, "http", "event", "http_request_end", "request_id", id,
			"method", request.Method, "duration_ms", time.Since(started).Milliseconds())
	})
}
