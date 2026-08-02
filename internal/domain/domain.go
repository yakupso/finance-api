// Package domain содержит сущности предметной области и доменные ошибки.
package domain

import (
	"time"

	"github.com/google/uuid"
)

// UserID - идентификатор владельца данных.
//
// Для инфо: отдельной таблицы пользователей в сервисе нет, считаем, что пользователь заводится во внешней системе
type UserID = uuid.UUID

// OperationType - направление движения денег.
type OperationType string

const (
	// OperationIncome - доход.
	OperationIncome OperationType = "income"
	// OperationExpense - расход.
	OperationExpense OperationType = "expense"
)

// IsValid сообщает, входит ли значение в множество допустимых.
func (t OperationType) IsValid() bool {
	return t == OperationIncome || t == OperationExpense
}

// String возвращает строковое представление типа операции.
func (t OperationType) String() string { return string(t) }

// OperationTypes возвращает все допустимые типы операций.
// Используется в сообщениях об ошибках валидации.
func OperationTypes() []OperationType {
	return []OperationType{OperationIncome, OperationExpense}
}

// Category - категория финансовых операций, принадлежащая пользователю.
type Category struct {
	ID        int64
	UserID    UserID
	Name      string
	CreatedAt time.Time
}

// CategoryRef - категория в составе операции: только то, что нужно для вывода.
type CategoryRef struct {
	ID   int64
	Name string
}

// Operation - зафиксированная финансовая операция.
//
// Amount хранится в минорных единицах (копейках) и всегда положителен;
// направление движения денег несёт поле Type.
type Operation struct {
	ID         int64
	UserID     UserID
	Category   CategoryRef
	Type       OperationType
	Amount     int64
	Comment    *string
	OccurredAt time.Time
	CreatedAt  time.Time
}

// NewCategory - данные для создания категории.
type NewCategory struct {
	UserID UserID
	Name   string
}

// NewOperation - данные для создания операции.
type NewOperation struct {
	UserID     UserID
	CategoryID int64
	Type       OperationType
	Amount     int64
	Comment    *string

	// OccurredAt == nil означает «дата не указана»: сервис подставит текущее
	// время. Признак намеренно выражен указателем, а не нулевым time.Time:
	// строка "0001-01-01T00:00:00Z" разбирается ровно в нулевое значение,
	// и по IsZero() её нельзя было бы отличить от отсутствующего поля -
	// заведомо некорректная дата молча заменялась бы на текущую вместо отказа.
	//
	// После Validate поле гарантированно не nil.
	OccurredAt *time.Time
}

// Period - временной интервал с включающими границами: From <= t <= To.
//
// Нормализация пользовательского ввода (дата без времени, порядок границ)
// выполняется в transport; сюда период приходит уже разрешённым в конкретные
// моменты времени.
type Period struct {
	From time.Time
	To   time.Time
}

// OperationFilter - условия выборки списка операций.
// Все поля, кроме UserID, опциональны; nil означает «без фильтра».
type OperationFilter struct {
	UserID     UserID
	From       *time.Time
	To         *time.Time
	Type       *OperationType
	CategoryID *int64
}

// CategoryExpense - сумма расходов по одной категории за период.
type CategoryExpense struct {
	CategoryID int64
	Category   string
	Amount     int64
}

// Stats - агрегированная статистика пользователя за период.
//
// Все суммы - в минорных единицах. Balance может быть отрицательным.
type Stats struct {
	Period             Period
	TotalIncome        int64
	TotalExpense       int64
	Balance            int64
	ExpensesByCategory []CategoryExpense
}
