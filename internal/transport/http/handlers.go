// Package http содержит HTTP-транспорт: маршрутизацию, разбор запросов,
// формирование ответов и сопоставление ошибок со статусами.
//
// Слой не содержит бизнес-логики: его задача - перевести HTTP-запрос в вызов
// сервиса и результат обратно в HTTP-ответ.
package http

import (
	"context"
	"log/slog"
	"net/http"

	"finance-api/internal/domain"
	"finance-api/internal/transport/http/middleware"
)

// Сервисы объявлены интерфейсами на стороне потребителя: транспорт
// тестируется без БД, с подставными реализациями.

// CategoryService - прикладные операции над категориями.
type CategoryService interface {
	Create(ctx context.Context, in domain.NewCategory) (domain.Category, error)
	List(ctx context.Context, userID domain.UserID) ([]domain.Category, error)
}

// OperationService - прикладные операции над финансовыми операциями.
type OperationService interface {
	Create(ctx context.Context, in domain.NewOperation) (domain.Operation, error)
	List(ctx context.Context, filter domain.OperationFilter) ([]domain.Operation, error)
}

// StatsService - агрегированная статистика.
type StatsService interface {
	Get(ctx context.Context, userID domain.UserID, period domain.Period) (domain.Stats, error)
}

// Handlers - набор HTTP-обработчиков API.
type Handlers struct {
	categories CategoryService
	operations OperationService
	stats      StatsService
	log        *slog.Logger
}

// NewHandlers создаёт обработчики.
func NewHandlers(
	categories CategoryService,
	operations OperationService,
	stats StatsService,
	log *slog.Logger,
) *Handlers {
	return &Handlers{categories: categories, operations: operations, stats: stats, log: log}
}

// createCategory обрабатывает POST /api/v1/categories.
func (h *Handlers) createCategory(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserID(r.Context())
	if !ok {
		h.respondError(w, r, errMissingUserContext)
		return
	}

	var req createCategoryRequest
	if err := decodeJSON(w, r, &req); err != nil {
		h.respondError(w, r, err)
		return
	}

	category, err := h.categories.Create(r.Context(), domain.NewCategory{
		UserID: userID,
		Name:   req.Name,
	})
	if err != nil {
		h.respondError(w, r, err)
		return
	}

	writeJSON(w, r, http.StatusCreated, toCategoryResponse(category), h.log)
}

// listCategories обрабатывает GET /api/v1/categories.
func (h *Handlers) listCategories(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserID(r.Context())
	if !ok {
		h.respondError(w, r, errMissingUserContext)
		return
	}

	categories, err := h.categories.List(r.Context(), userID)
	if err != nil {
		h.respondError(w, r, err)
		return
	}

	writeJSON(w, r, http.StatusOK, toCategoryListResponse(categories), h.log)
}

// createOperation обрабатывает POST /api/v1/operations.
func (h *Handlers) createOperation(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserID(r.Context())
	if !ok {
		h.respondError(w, r, errMissingUserContext)
		return
	}

	var req createOperationRequest
	if err := decodeJSON(w, r, &req); err != nil {
		h.respondError(w, r, err)
		return
	}

	in, err := req.toDomain(userID)
	if err != nil {
		h.respondError(w, r, err)
		return
	}

	operation, err := h.operations.Create(r.Context(), in)
	if err != nil {
		h.respondError(w, r, err)
		return
	}

	writeJSON(w, r, http.StatusCreated, toOperationResponse(operation), h.log)
}

// listOperations обрабатывает GET /api/v1/operations.
func (h *Handlers) listOperations(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserID(r.Context())
	if !ok {
		h.respondError(w, r, errMissingUserContext)
		return
	}

	query := r.URL.Query()

	from, to, err := parsePeriodBounds(query)
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	opType, err := parseOperationType(query)
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	categoryID, err := parseCategoryID(query)
	if err != nil {
		h.respondError(w, r, err)
		return
	}

	operations, err := h.operations.List(r.Context(), domain.OperationFilter{
		UserID:     userID,
		From:       from,
		To:         to,
		Type:       opType,
		CategoryID: categoryID,
	})
	if err != nil {
		h.respondError(w, r, err)
		return
	}

	writeJSON(w, r, http.StatusOK, toOperationListResponse(operations), h.log)
}

// getStats обрабатывает GET /api/v1/stats.
func (h *Handlers) getStats(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserID(r.Context())
	if !ok {
		h.respondError(w, r, errMissingUserContext)
		return
	}

	period, err := parseRequiredPeriod(r.URL.Query())
	if err != nil {
		h.respondError(w, r, err)
		return
	}

	stats, err := h.stats.Get(r.Context(), userID, period)
	if err != nil {
		h.respondError(w, r, err)
		return
	}

	writeJSON(w, r, http.StatusOK, toStatsResponse(stats), h.log)
}

// health обрабатывает GET /healthz.
//
// Проба живости: отвечает, пока процесс способен обслуживать запросы.
// Состояние БД намеренно не проверяется - это задача readiness-пробы,
// которая в объём задания не входит.
func (h *Handlers) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, r, http.StatusOK, map[string]string{"status": "ok"}, h.log)
}

// errMissingUserContext означает, что хендлер вызван в обход middleware
// RequireUserID. Для смонтированных маршрутов это невозможно, поэтому 500.
var errMissingUserContext = newAPIError(http.StatusInternalServerError, codeInternalError,
	"user context is missing")
