package middleware

import (
	"net/http"
)

func CORSMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 设置允许的源（开发环境允许所有源，生产环境应该限制）
		w.Header().Set("Access-Control-Allow-Origin", "*")

		// 设置允许的 HTTP 方法
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")

		// 设置允许的请求头
		w.Header().Set("Access-Control-Allow-Headers", "Origin, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With, Idempotency-Key, X-Request-ID, Last-Event-ID")

		// 允许携带凭证（如果需要）
		w.Header().Set("Access-Control-Allow-Credentials", "true")

		// 预检请求缓存时间（秒）
		w.Header().Set("Access-Control-Max-Age", "3600")

		// 处理 OPTIONS 预检请求
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		// 继续处理后续请求
		next.ServeHTTP(w, r)
	})
}
