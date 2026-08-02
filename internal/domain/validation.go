package domain

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

// Ограничения предметной области. Продублированы CHECK-ограничениями в схеме БД:
// проверка здесь даёт понятное сообщение об ошибке, проверка в БД гарантирует
// целостность данных даже при записи в обход API.
const (
	// MaxCategoryNameLen - максимальная длина названия категории в символах.
	MaxCategoryNameLen = 100
	// MaxCommentLen - максимальная длина комментария к операции в символах.
	MaxCommentLen = 500

	// MinAmount - минимальная сумма операции в минорных единицах (1 копейка).
	MinAmount int64 = 1
	// MaxAmount - максимальная сумма операции в минорных единицах.
	//
	// Верхняя граница выбрана так, чтобы значение оставалось точно
	// представимым числом в JSON (предел безопасного целого - 2^53-1),
	// включая суммы агрегатов. Это ~1 трлн денежных единиц.
	MaxAmount int64 = 99_999_999_999_999
)

// MaxOccurredAtAhead - насколько операция может быть датирована будущим.
// Бэкдейтинг не ограничен сверху (кроме MinOccurredAt), а вот дата на десять
// лет вперёд почти наверняка опечатка.
const MaxOccurredAtAhead = 365 * 24 * time.Hour

// MinOccurredAt - нижняя граница даты операции. Отсекает мусор вроде года 0001,
// который иначе молча попал бы в БД и испортил статистику.
var MinOccurredAt = time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC)

// ValidationErrors - набор нарушений бизнес-правил, собранных за один проход.
//
// Возвращаются все найденные ошибки сразу: клиенту не нужно повторять запрос,
// чтобы узнать про второе некорректное поле.
type ValidationErrors []*ValidationError

// Error реализует интерфейс error.
func (e ValidationErrors) Error() string {
	if len(e) == 0 {
		return "validation failed"
	}
	parts := make([]string, 0, len(e))
	for _, item := range e {
		parts = append(parts, item.Error())
	}
	return strings.Join(parts, "; ")
}

// Unwrap позволяет errors.As добраться до отдельных ошибок валидации.
func (e ValidationErrors) Unwrap() []error {
	out := make([]error, 0, len(e))
	for _, item := range e {
		out = append(out, item)
	}
	return out
}

// AsValidationErrors извлекает набор ошибок валидации из цепочки ошибок.
func AsValidationErrors(err error) (ValidationErrors, bool) {
	var multi ValidationErrors
	if errors.As(err, &multi) {
		return multi, true
	}
	var single *ValidationError
	if errors.As(err, &single) {
		return ValidationErrors{single}, true
	}
	return nil, false
}

// errorsOrNil возвращает nil-интерфейс при пустом наборе.
// Прямой возврат ValidationErrors(nil) дал бы ненулевой интерфейс,
// у которого err != nil при пустом значении - классическая ловушка Go.
func errorsOrNil(errs ValidationErrors) error {
	if len(errs) == 0 {
		return nil
	}
	return errs
}

// NormalizeCategoryName приводит название категории к каноническому виду:
// убирает окружающие пробелы и схлопывает внутренние последовательности
// пробельных символов в один пробел.
func NormalizeCategoryName(name string) string {
	return strings.Join(strings.Fields(name), " ")
}

// Validate проверяет данные для создания категории.
// Ожидается, что имя уже нормализовано через NormalizeCategoryName.
func (in NewCategory) Validate() error {
	var errs ValidationErrors

	switch {
	case in.Name == "":
		errs = append(errs, NewValidationError("name", "is required and must not be blank"))
	case utf8.RuneCountInString(in.Name) > MaxCategoryNameLen:
		errs = append(errs, NewValidationError("name",
			"must be at most %d characters, got %d", MaxCategoryNameLen, utf8.RuneCountInString(in.Name)))
	}

	if in.UserID == (UserID{}) {
		errs = append(errs, NewValidationError("user_id", "is required"))
	}

	return errorsOrNil(errs)
}

// Validate проверяет данные для создания операции.
// Параметр now передаётся явно, чтобы правило про дату в будущем было
// детерминированно проверяемо в тестах.
func (in NewOperation) Validate(now time.Time) error {
	var errs ValidationErrors

	if in.UserID == (UserID{}) {
		errs = append(errs, NewValidationError("user_id", "is required"))
	}

	if in.CategoryID <= 0 {
		errs = append(errs, NewValidationError("category_id", "is required and must be a positive integer"))
	}

	if !in.Type.IsValid() {
		errs = append(errs, NewValidationError("type",
			"must be one of [%s], got %q", joinTypes(OperationTypes()), in.Type))
	}

	switch {
	case in.Amount < MinAmount:
		errs = append(errs, NewValidationError("amount",
			"must be a positive integer in minor units (kopecks), got %d", in.Amount))
	case in.Amount > MaxAmount:
		errs = append(errs, NewValidationError("amount",
			"must be at most %d minor units, got %d", MaxAmount, in.Amount))
	}

	if in.Comment != nil && utf8.RuneCountInString(*in.Comment) > MaxCommentLen {
		errs = append(errs, NewValidationError("comment",
			"must be at most %d characters, got %d", MaxCommentLen, utf8.RuneCountInString(*in.Comment)))
	}

	switch {
	case in.OccurredAt == nil:
		// Сервис подставляет текущее время до вызова Validate, поэтому сюда
		// можно попасть только при прямом вызове в обход сервиса.
		errs = append(errs, NewValidationError("occurred_at", "is required"))
	case in.OccurredAt.Before(MinOccurredAt):
		errs = append(errs, NewValidationError("occurred_at",
			"must not be earlier than %s", MinOccurredAt.Format(time.RFC3339)))
	case in.OccurredAt.After(now.Add(MaxOccurredAtAhead)):
		errs = append(errs, NewValidationError("occurred_at",
			"must not be more than %d days in the future", int(MaxOccurredAtAhead.Hours()/24)))
	}

	return errorsOrNil(errs)
}

func joinTypes(types []OperationType) string {
	parts := make([]string, 0, len(types))
	for _, t := range types {
		parts = append(parts, t.String())
	}
	return strings.Join(parts, " ")
}
