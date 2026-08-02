package http

import (
	"time"

	"finance-api/internal/domain"
)

// DTO транспортного слоя отделены от доменных сущностей намеренно: форма
// ответа API - часть публичного контракта, и она не должна меняться только
// потому, что во внутренней модели переименовали поле.

// --------------------------------------------------------------- категории --

type createCategoryRequest struct {
	Name string `json:"name"`
}

type categoryResponse struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

type categoryListResponse struct {
	Categories []categoryResponse `json:"categories"`
}

func toCategoryResponse(c domain.Category) categoryResponse {
	return categoryResponse{ID: c.ID, Name: c.Name, CreatedAt: c.CreatedAt}
}

func toCategoryListResponse(categories []domain.Category) categoryListResponse {
	// Именно make, а не var: nil-срез сериализуется в null, а в ответе должен
	// быть пустой массив.
	items := make([]categoryResponse, 0, len(categories))
	for _, c := range categories {
		items = append(items, toCategoryResponse(c))
	}
	return categoryListResponse{Categories: items}
}

// ---------------------------------------------------------------- операции --

// createOperationRequest - тело запроса на создание операции.
//
// Amount - целое число в минорных единицах (копейках): 150000 - это 1500.00.
// Хранение денег в int64 исключает ошибки округления, неизбежные при float,
// и позволяет PostgreSQL складывать суммы точно.
//
// Указатели у Amount и OccurredAt различают «поле не передано» и «передан
// ноль/пустое значение»: без этого amount=0 нельзя было бы отличить от
// отсутствующего поля и сообщить об этом клиенту.
type createOperationRequest struct {
	CategoryID *int64  `json:"category_id"`
	Type       string  `json:"type"`
	Amount     *int64  `json:"amount"`
	Comment    *string `json:"comment"`
	OccurredAt *string `json:"occurred_at"`
}

// toDomain переводит запрос в доменную структуру.
//
// Проверяется только то, что нельзя выразить в доменных правилах, - формат
// метки времени. Остальное валидирует домен: единый набор правил на все точки
// входа и единый формат ответа об ошибке.
func (req createOperationRequest) toDomain(userID domain.UserID) (domain.NewOperation, error) {
	in := domain.NewOperation{
		UserID:  userID,
		Type:    domain.OperationType(req.Type),
		Comment: req.Comment,
	}
	if req.CategoryID != nil {
		in.CategoryID = *req.CategoryID
	}
	if req.Amount != nil {
		in.Amount = *req.Amount
	}

	if req.OccurredAt != nil && *req.OccurredAt != "" {
		occurredAt, err := time.Parse(time.RFC3339, *req.OccurredAt)
		if err != nil {
			return domain.NewOperation{}, badRequest(codeBadRequest,
				"occurred_at must be an RFC 3339 timestamp (2006-01-02T15:04:05Z), got %q",
				*req.OccurredAt)
		}
		in.OccurredAt = &occurredAt
	}
	// Если поле не передано, OccurredAt остаётся nil и сервис подставит
	// текущее время. Переданное значение сюда доходит как есть, даже если оно
	// совпадает с нулевым time.Time, - отвергнет его уже валидация домена.

	return in, nil
}

type operationResponse struct {
	ID         int64            `json:"id"`
	Type       string           `json:"type"`
	Amount     int64            `json:"amount"`
	Category   categoryRefValue `json:"category"`
	Comment    *string          `json:"comment,omitempty"`
	OccurredAt time.Time        `json:"occurred_at"`
	CreatedAt  time.Time        `json:"created_at"`
}

type categoryRefValue struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type operationListResponse struct {
	Operations []operationResponse `json:"operations"`
}

func toOperationResponse(op domain.Operation) operationResponse {
	return operationResponse{
		ID:         op.ID,
		Type:       op.Type.String(),
		Amount:     op.Amount,
		Category:   categoryRefValue{ID: op.Category.ID, Name: op.Category.Name},
		Comment:    op.Comment,
		OccurredAt: op.OccurredAt,
		CreatedAt:  op.CreatedAt,
	}
}

func toOperationListResponse(operations []domain.Operation) operationListResponse {
	items := make([]operationResponse, 0, len(operations))
	for _, op := range operations {
		items = append(items, toOperationResponse(op))
	}
	return operationListResponse{Operations: items}
}

// -------------------------------------------------------------- статистика --

type statsResponse struct {
	Period             periodResponse            `json:"period"`
	TotalIncome        int64                     `json:"total_income"`
	TotalExpense       int64                     `json:"total_expense"`
	Balance            int64                     `json:"balance"`
	ExpensesByCategory []categoryExpenseResponse `json:"expenses_by_category"`
}

// periodResponse возвращает фактически применённые границы периода после
// нормализации: клиенту не нужно догадываться, как был истолкован его ввод.
type periodResponse struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

type categoryExpenseResponse struct {
	CategoryID int64  `json:"category_id"`
	Category   string `json:"category"`
	Amount     int64  `json:"amount"`
}

func toStatsResponse(s domain.Stats) statsResponse {
	items := make([]categoryExpenseResponse, 0, len(s.ExpensesByCategory))
	for _, e := range s.ExpensesByCategory {
		items = append(items, categoryExpenseResponse{
			CategoryID: e.CategoryID,
			Category:   e.Category,
			Amount:     e.Amount,
		})
	}
	return statsResponse{
		// Период приходит из query-параметров запроса и может нести
		// произвольное смещение; в ответе, как и все прочие метки времени,
		// он приводится к UTC.
		Period:             periodResponse{From: s.Period.From.UTC(), To: s.Period.To.UTC()},
		TotalIncome:        s.TotalIncome,
		TotalExpense:       s.TotalExpense,
		Balance:            s.Balance,
		ExpensesByCategory: items,
	}
}
