package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/nekoimi/scrapio/internal/pkg/error_ext"
	"github.com/nekoimi/scrapio/internal/pkg/jwt"
	"github.com/nekoimi/scrapio/internal/pkg/request"
	"github.com/nekoimi/scrapio/internal/pkg/respond"
	log "github.com/sirupsen/logrus"
)

func AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("Authorization")
		if token == "" {
			if c, err := r.Cookie("token"); err == nil {
				token = c.Value
			} else {
				log.Debugf("获取请求cookie异常: %s", err.Error())
			}
		}

		if token == "" {
			if strings.HasPrefix(r.URL.Path, "/api/v3") {
				respond.V3Error(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "身份认证异常", false)
				return
			}
			respond.Error(w, error_ext.AuthenticationError)
			return
		}

		sub, err := jwt.ParseToken(token)
		if err != nil {
			if strings.HasPrefix(r.URL.Path, "/api/v3") {
				respond.V3Error(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "认证信息无效或已过期", false)
				return
			}
			if errors.Is(err, jwt.TokenExpireError) {
				respond.Error(w, error_ext.AuthenticationExpirseError)
			} else {
				respond.Error(w, error_ext.AuthenticationError)
			}
			return
		}

		authCtx := context.WithValue(r.Context(), request.ContextJwtUser, sub)
		next.ServeHTTP(w, r.WithContext(authCtx))
	})
}
