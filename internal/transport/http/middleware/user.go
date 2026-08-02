// Package middleware содержит HTTP-middleware, специфичные для сервиса.
package middleware

import (
	"context"
	"net/http"

	"finance-api/internal/domain"

	"github.com/google/uuid"
)

// UserIDHeader - заголовок, в котором передаётся идентификатор пользователя.
//
// Аутентификация по условиям задания не реализуется, поэтому принадлежность
// данных определяется этим значением напрямую. Заголовок выбран вместо
// query-параметра ради единообразия: иначе для POST-запросов идентификатор
// пришлось бы либо дублировать в теле, либо смешивать два способа передачи.
const UserIDHeader = "X-User-Id"

type contextKey struct{}

var userIDKey contextKey

// ErrorWriter пишет ответ об ошибке. Реализуется транспортным слоем, чтобы
// middleware не дублировал формат конверта ошибки.
type ErrorWriter func(w http.ResponseWriter, r *http.Request, status int, code, message string)

// RequireUserID извлекает и валидирует идентификатор пользователя из заголовка,
// после чего кладёт его в контекст запроса.
//
// Отсутствующий или некорректный идентификатор - ошибка запроса как такового,
// поэтому 400, а не 401: авторизации в сервисе нет.
func RequireUserID(writeError ErrorWriter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := r.Header.Get(UserIDHeader)
			if raw == "" {
				writeError(w, r, http.StatusBadRequest, "missing_user_id",
					UserIDHeader+" header is required and must contain a UUID")
				return
			}

			userID, err := uuid.Parse(raw)
			if err != nil {
				writeError(w, r, http.StatusBadRequest, "invalid_user_id",
					UserIDHeader+" header must be a valid UUID")
				return
			}
			if userID == uuid.Nil {
				writeError(w, r, http.StatusBadRequest, "invalid_user_id",
					UserIDHeader+" header must not be the nil UUID")
				return
			}

			ctx := context.WithValue(r.Context(), userIDKey, userID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// UserID возвращает идентификатор пользователя из контекста запроса.
//
// Второе значение - false, если middleware RequireUserID не отработал;
// для хендлеров, смонтированных под этим middleware, это невозможно.
func UserID(ctx context.Context) (domain.UserID, bool) {
	userID, ok := ctx.Value(userIDKey).(uuid.UUID)
	return userID, ok
}
