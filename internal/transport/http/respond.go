package http

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
)

// maxRequestBody - предел размера тела запроса. Тела в этом API - небольшие
// JSON-объекты, поэтому ограничение защищает от случайной или намеренной
// отправки многомегабайтного payload'а.
const maxRequestBody = 1 << 20 // 1 MiB

// writeJSON сериализует ответ и отправляет его клиенту.
//
// Ошибка кодирования уже не может быть передана клиенту корректно - статус
// и часть тела отправлены, - поэтому она только логируется.
func writeJSON(w http.ResponseWriter, r *http.Request, status int, payload any, log *slog.Logger) {
	body, err := json.Marshal(payload)
	if err != nil {
		log.ErrorContext(r.Context(), "encode response failed",
			slog.String("path", r.URL.Path),
			slog.Any("error", err),
		)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"code":"internal_error","message":"internal server error"}}`))
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if _, err := w.Write(body); err != nil {
		log.DebugContext(r.Context(), "write response failed", slog.Any("error", err))
	}
}

// decodeJSON разбирает тело запроса в dst.
//
// DisallowUnknownFields включён намеренно: опечатка в имени поля иначе была бы
// молча проигнорирована, и клиент получил бы операцию с нулевой суммой вместо
// внятного сообщения о том, какое поле он назвал неверно.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	if err := requireJSONContentType(r); err != nil {
		return err
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(dst); err != nil {
		return decodeError(err)
	}

	// В теле должен быть ровно один JSON-объект.
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return badRequest(codeBadRequest, "request body must contain exactly one JSON object")
	}
	return nil
}

// decodeError переводит ошибку разбора JSON в понятное клиенту сообщение.
func decodeError(err error) error {
	var (
		syntaxErr    *json.SyntaxError
		typeErr      *json.UnmarshalTypeError
		maxBytesErr  *http.MaxBytesError
		invalidUnmar *json.InvalidUnmarshalError
	)

	switch {
	case errors.As(err, &syntaxErr):
		return badRequest(codeBadRequest,
			"request body contains malformed JSON at offset %d", syntaxErr.Offset)

	case errors.Is(err, io.ErrUnexpectedEOF):
		return badRequest(codeBadRequest, "request body contains malformed JSON")

	case errors.As(err, &typeErr):
		if typeErr.Field != "" {
			return badRequest(codeBadRequest,
				"field %q must be of type %s, got %s",
				typeErr.Field, jsonTypeName(typeErr.Type), typeErr.Value)
		}
		return badRequest(codeBadRequest, "request body has an unexpected type")

	case errors.Is(err, io.EOF):
		return badRequest(codeBadRequest, "request body must not be empty")

	case errors.As(err, &maxBytesErr):
		return newAPIError(http.StatusRequestEntityTooLarge, codeRequestTooLarge,
			"request body must not exceed %d bytes", maxBytesErr.Limit)

	case strings.HasPrefix(err.Error(), "json: unknown field "):
		field := strings.TrimPrefix(err.Error(), "json: unknown field ")
		return badRequest(codeBadRequest, "request body contains unknown field %s", field)

	case errors.As(err, &invalidUnmar):
		// Ошибка программиста, а не клиента.
		return fmt.Errorf("decode json: %w", err)

	default:
		return badRequest(codeBadRequest, "request body could not be decoded")
	}
}

// jsonTypeName переводит Go-тип в название типа JSON.
//
// Клиент API рассуждает в терминах JSON и не должен видеть в сообщении
// об ошибке внутренние типы вроде int64 или *string.
func jsonTypeName(t reflect.Type) string {
	if t == nil {
		return "value"
	}
	switch t.Kind() {
	case reflect.Pointer:
		return jsonTypeName(t.Elem())
	case reflect.Bool:
		return "boolean"
	case reflect.String:
		return "string"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return "number"
	case reflect.Slice, reflect.Array:
		return "array"
	case reflect.Map, reflect.Struct:
		return "object"
	default:
		return "value"
	}
}

// requireJSONContentType проверяет заголовок Content-Type у запросов с телом.
func requireJSONContentType(r *http.Request) error {
	contentType := r.Header.Get("Content-Type")
	if contentType == "" {
		return newAPIError(http.StatusUnsupportedMediaType, codeUnsupportedMediaType,
			"Content-Type header is required and must be application/json")
	}
	// Отсекаем параметры вида "; charset=utf-8".
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	if mediaType != "application/json" {
		return newAPIError(http.StatusUnsupportedMediaType, codeUnsupportedMediaType,
			"Content-Type must be application/json, got %q", mediaType)
	}
	return nil
}
