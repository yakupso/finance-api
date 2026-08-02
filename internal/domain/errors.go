package domain

import (
	"errors"
	"fmt"
)

// Доменные ошибки. Слои выше сопоставляют их с HTTP-статусами через errors.Is,
// поэтому репозиторий не знает о кодах ответа, а хендлеры - о кодах PostgreSQL.
var (
	// ErrCategoryNotFound - категория не существует либо принадлежит другому
	// пользователю. Эти два случая намеренно неразличимы снаружи: иначе API
	// позволял бы перебором узнавать о существовании чужих категорий.
	ErrCategoryNotFound = errors.New("category not found")

	// ErrCategoryAlreadyExists - у пользователя уже есть категория с таким
	// именем (сравнение регистронезависимое).
	ErrCategoryAlreadyExists = errors.New("category already exists")
)

// ValidationError - нарушение бизнес-правила в конкретном поле.
//
// Транспортный слой разворачивает её в элемент массива details ответа об ошибке.
type ValidationError struct {
	Field   string
	Message string
}

// NewValidationError создаёт ошибку валидации для поля.
func NewValidationError(field, format string, args ...any) *ValidationError {
	return &ValidationError{Field: field, Message: fmt.Sprintf(format, args...)}
}

// Error реализует интерфейс error.
func (e *ValidationError) Error() string {
	if e.Field == "" {
		return e.Message
	}
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}
