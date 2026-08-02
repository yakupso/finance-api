package http

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"finance-api/internal/domain"
)

// Коды ошибок API. Стабильны и предназначены для программной обработки
// клиентом - в отличие от message, который может меняться.
const (
	codeBadRequest           = "bad_request"
	codeInvalidQueryParam    = "invalid_query_param"
	codeValidationError      = "validation_error"
	codeCategoryNotFound     = "category_not_found"
	codeCategoryExists       = "category_already_exists"
	codeNotFound             = "not_found"
	codeMethodNotAllowed     = "method_not_allowed"
	codeUnsupportedMediaType = "unsupported_media_type"
	codeRequestTooLarge      = "request_too_large"
	codeInternalError        = "internal_error"
)

// errorResponse - единый конверт ошибки для всех эндпоинтов.
type errorResponse struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code    string       `json:"code"`
	Message string       `json:"message"`
	Details []fieldError `json:"details,omitempty"`
}

// fieldError - нарушение в конкретном поле запроса.
type fieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// apiError - ошибка транспортного уровня с уже определённым HTTP-статусом.
// Используется там, где проблема относится к запросу целиком (заголовки,
// query-параметры, формат тела), а не к бизнес-правилам.
type apiError struct {
	Status  int
	Code    string
	Message string
}

func (e *apiError) Error() string { return e.Message }

func newAPIError(status int, code, format string, args ...any) *apiError {
	return &apiError{Status: status, Code: code, Message: fmt.Sprintf(format, args...)}
}

// badRequest - сокращение для самой частой ошибки транспортного уровня.
func badRequest(code, format string, args ...any) *apiError {
	return newAPIError(http.StatusBadRequest, code, format, args...)
}

// writeError отправляет ошибку в едином формате.
func (h *Handlers) writeError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	writeJSON(w, r, status, errorResponse{Error: errorBody{Code: code, Message: message}}, h.log)
}

// respondError сопоставляет ошибку любого слоя с HTTP-ответом.
//
// Это единственное место, где принимается решение о статусе ответа.
// Разделение проведено по одному правилу:
//
//	400 - с запросом что-то не так как с запросом: битый JSON, некорректный
//	      заголовок, неразбираемые query-параметры;
//	422 - тело запроса успешно разобрано, но нарушает бизнес-правила.
func (h *Handlers) respondError(w http.ResponseWriter, r *http.Request, err error) {
	// Ошибки валидации домена -> 422 с перечнем полей.
	if validationErrs, ok := domain.AsValidationErrors(err); ok {
		details := make([]fieldError, 0, len(validationErrs))
		for _, item := range validationErrs {
			details = append(details, fieldError{Field: item.Field, Message: item.Message})
		}
		writeJSON(w, r, http.StatusUnprocessableEntity, errorResponse{Error: errorBody{
			Code:    codeValidationError,
			Message: "request body failed validation",
			Details: details,
		}}, h.log)
		return
	}

	// Ошибки транспортного уровня несут статус в себе.
	var apiErr *apiError
	if errors.As(err, &apiErr) {
		h.writeError(w, r, apiErr.Status, apiErr.Code, apiErr.Message)
		return
	}

	// Доменные ошибки.
	switch {
	case errors.Is(err, domain.ErrCategoryNotFound):
		// 422, а не 404: запрошенный ресурс (/operations) существует,
		// некорректна ссылка внутри тела запроса.
		writeJSON(w, r, http.StatusUnprocessableEntity, errorResponse{Error: errorBody{
			Code:    codeCategoryNotFound,
			Message: "category does not exist or belongs to another user",
			Details: []fieldError{{Field: "category_id", Message: "unknown category"}},
		}}, h.log)
		return

	case errors.Is(err, domain.ErrCategoryAlreadyExists):
		h.writeError(w, r, http.StatusConflict, codeCategoryExists,
			"category with this name already exists")
		return
	}

	// Всё остальное - наша вина. Наружу уходит только общая формулировка,
	// подробности пишутся в лог: детали ошибки БД клиенту знать незачем.
	h.log.ErrorContext(r.Context(), "unhandled error",
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
		slog.Any("error", err),
	)
	h.writeError(w, r, http.StatusInternalServerError, codeInternalError,
		"internal server error")
}
