package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"finance-api/internal/domain"
	transport "finance-api/internal/transport/http"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testUserID = "11111111-1111-4111-8111-111111111111"

var (
	testUser  = uuid.MustParse(testUserID)
	fixedTime = time.Date(2026, time.July, 20, 12, 0, 0, 0, time.UTC)
)

// ------------------------------------------------------------ подставные ----

type stubCategoryService struct {
	createFn func(ctx context.Context, in domain.NewCategory) (domain.Category, error)
	listFn   func(ctx context.Context, userID domain.UserID) ([]domain.Category, error)
	gotIn    domain.NewCategory
}

func (s *stubCategoryService) Create(ctx context.Context, in domain.NewCategory) (domain.Category, error) {
	s.gotIn = in
	if s.createFn != nil {
		return s.createFn(ctx, in)
	}
	return domain.Category{ID: 1, UserID: in.UserID, Name: in.Name, CreatedAt: fixedTime}, nil
}

func (s *stubCategoryService) List(ctx context.Context, userID domain.UserID) ([]domain.Category, error) {
	if s.listFn != nil {
		return s.listFn(ctx, userID)
	}
	return nil, nil
}

type stubOperationService struct {
	createFn  func(ctx context.Context, in domain.NewOperation) (domain.Operation, error)
	listFn    func(ctx context.Context, filter domain.OperationFilter) ([]domain.Operation, error)
	gotIn     domain.NewOperation
	gotFilter domain.OperationFilter
}

func (s *stubOperationService) Create(ctx context.Context, in domain.NewOperation) (domain.Operation, error) {
	s.gotIn = in
	if s.createFn != nil {
		return s.createFn(ctx, in)
	}
	occurredAt := fixedTime
	if in.OccurredAt != nil {
		occurredAt = *in.OccurredAt
	}
	return domain.Operation{
		ID:         101,
		UserID:     in.UserID,
		Category:   domain.CategoryRef{ID: in.CategoryID, Name: "Продукты"},
		Type:       in.Type,
		Amount:     in.Amount,
		Comment:    in.Comment,
		OccurredAt: occurredAt,
		CreatedAt:  fixedTime,
	}, nil
}

func (s *stubOperationService) List(ctx context.Context, filter domain.OperationFilter) ([]domain.Operation, error) {
	s.gotFilter = filter
	if s.listFn != nil {
		return s.listFn(ctx, filter)
	}
	return nil, nil
}

type stubStatsService struct {
	getFn func(ctx context.Context, userID domain.UserID, period domain.Period) (domain.Stats, error)
}

func (s *stubStatsService) Get(ctx context.Context, userID domain.UserID, period domain.Period) (domain.Stats, error) {
	if s.getFn != nil {
		return s.getFn(ctx, userID, period)
	}
	return domain.Stats{Period: period}, nil
}

// ---------------------------------------------------------------- каркас ----

type stubs struct {
	categories *stubCategoryService
	operations *stubOperationService
	stats      *stubStatsService
}

func newServer(t *testing.T) (http.Handler, *stubs) {
	t.Helper()

	s := &stubs{
		categories: &stubCategoryService{},
		operations: &stubOperationService{},
		stats:      &stubStatsService{},
	}
	// Логи тестов не должны засорять вывод; уровень Error+ пишется в io.Discard.
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	handlers := transport.NewHandlers(s.categories, s.operations, s.stats, log)
	return transport.NewRouter(handlers), s
}

type request struct {
	method string
	path   string
	body   string
	userID string // "-" - заголовок не отправляется вовсе
	ctype  string
}

func do(t *testing.T, handler http.Handler, req request) *httptest.ResponseRecorder {
	t.Helper()

	var body io.Reader
	if req.body != "" {
		body = strings.NewReader(req.body)
	}

	httpReq := httptest.NewRequest(req.method, req.path, body)

	switch req.userID {
	case "-":
		// заголовок намеренно отсутствует
	case "":
		httpReq.Header.Set("X-User-Id", testUserID)
	default:
		httpReq.Header.Set("X-User-Id", req.userID)
	}

	if req.ctype != "-" {
		ctype := req.ctype
		if ctype == "" {
			ctype = "application/json"
		}
		httpReq.Header.Set("Content-Type", ctype)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httpReq)
	return rec
}

// decodeError разбирает конверт ошибки и проверяет его форму.
func decodeError(t *testing.T, rec *httptest.ResponseRecorder) struct {
	Code    string
	Message string
	Details []struct {
		Field   string `json:"field"`
		Message string `json:"message"`
	}
} {
	t.Helper()

	var payload struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			Details []struct {
				Field   string `json:"field"`
				Message string `json:"message"`
			} `json:"details"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload), "тело: %s", rec.Body.String())

	assert.NotEmpty(t, payload.Error.Code, "код ошибки обязателен")
	assert.NotEmpty(t, payload.Error.Message, "сообщение об ошибке обязательно")
	assert.Equal(t, "application/json; charset=utf-8", rec.Header().Get("Content-Type"))

	return struct {
		Code    string
		Message string
		Details []struct {
			Field   string `json:"field"`
			Message string `json:"message"`
		}
	}{payload.Error.Code, payload.Error.Message, payload.Error.Details}
}

// ------------------------------------------------- таблица HTTP-статусов ----

// Прямая проверка требования ТЗ «обрабатывать ошибки и возвращать корректные
// HTTP-статусы»: каждая строка соответствует строке таблицы статусов в README.
func TestStatusCodeTable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		req        request
		wantStatus int
		wantCode   string
	}{
		{
			name:       "отсутствует X-User-Id",
			req:        request{method: "GET", path: "/api/v1/categories", userID: "-"},
			wantStatus: http.StatusBadRequest,
			wantCode:   "missing_user_id",
		},
		{
			name:       "X-User-Id не UUID",
			req:        request{method: "GET", path: "/api/v1/categories", userID: "not-a-uuid"},
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_user_id",
		},
		{
			name:       "X-User-Id нулевой UUID",
			req:        request{method: "GET", path: "/api/v1/categories", userID: uuid.Nil.String()},
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_user_id",
		},
		{
			name:       "неизвестный маршрут",
			req:        request{method: "GET", path: "/api/v1/unknown"},
			wantStatus: http.StatusNotFound,
			wantCode:   "not_found",
		},
		{
			name:       "метод не поддерживается",
			req:        request{method: "DELETE", path: "/api/v1/categories"},
			wantStatus: http.StatusMethodNotAllowed,
			wantCode:   "method_not_allowed",
		},
		{
			name: "Content-Type отсутствует",
			req: request{method: "POST", path: "/api/v1/categories",
				body: `{"name":"Продукты"}`, ctype: "-"},
			wantStatus: http.StatusUnsupportedMediaType,
			wantCode:   "unsupported_media_type",
		},
		{
			name: "Content-Type не JSON",
			req: request{method: "POST", path: "/api/v1/categories",
				body: `{"name":"Продукты"}`, ctype: "text/plain"},
			wantStatus: http.StatusUnsupportedMediaType,
			wantCode:   "unsupported_media_type",
		},
		{
			name: "Content-Type с параметром charset принимается",
			req: request{method: "POST", path: "/api/v1/categories",
				body: `{"name":"Продукты"}`, ctype: "application/json; charset=utf-8"},
			wantStatus: http.StatusCreated,
		},
		{
			name:       "битый JSON",
			req:        request{method: "POST", path: "/api/v1/categories", body: `{"name":`},
			wantStatus: http.StatusBadRequest,
			wantCode:   "bad_request",
		},
		{
			name:       "пустое тело",
			req:        request{method: "POST", path: "/api/v1/categories", ctype: "application/json"},
			wantStatus: http.StatusBadRequest,
			wantCode:   "bad_request",
		},
		{
			name:       "неизвестное поле в теле",
			req:        request{method: "POST", path: "/api/v1/categories", body: `{"nam":"Продукты"}`},
			wantStatus: http.StatusBadRequest,
			wantCode:   "bad_request",
		},
		{
			name:       "два JSON-объекта в теле",
			req:        request{method: "POST", path: "/api/v1/categories", body: `{"name":"a"}{"name":"b"}`},
			wantStatus: http.StatusBadRequest,
			wantCode:   "bad_request",
		},
		{
			name: "неверный тип поля",
			req: request{method: "POST", path: "/api/v1/operations",
				body: `{"category_id":"1","type":"expense","amount":100}`},
			wantStatus: http.StatusBadRequest,
			wantCode:   "bad_request",
		},
		{
			name: "occurred_at не по RFC 3339",
			req: request{method: "POST", path: "/api/v1/operations",
				body: `{"category_id":1,"type":"expense","amount":100,"occurred_at":"15.07.2026"}`},
			wantStatus: http.StatusBadRequest,
			wantCode:   "bad_request",
		},
		{
			name:       "некорректная дата в query",
			req:        request{method: "GET", path: "/api/v1/operations?from=вчера"},
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_query_param",
		},
		{
			name:       "неизвестный тип в query",
			req:        request{method: "GET", path: "/api/v1/operations?type=transfer"},
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_query_param",
		},
		{
			name:       "from позже to",
			req:        request{method: "GET", path: "/api/v1/stats?from=2026-08-01&to=2026-07-01"},
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_query_param",
		},
		{
			name:       "статистика без обязательного периода",
			req:        request{method: "GET", path: "/api/v1/stats"},
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_query_param",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handler, _ := newServer(t)
			rec := do(t, handler, tt.req)

			require.Equal(t, tt.wantStatus, rec.Code, "тело: %s", rec.Body.String())
			if tt.wantCode != "" {
				assert.Equal(t, tt.wantCode, decodeError(t, rec).Code)
			}
		})
	}
}

// Доменные ошибки сервисов должны превращаться в заданные статусы.
func TestDomainErrorMapping(t *testing.T) {
	t.Parallel()

	t.Run("категория уже существует -> 409", func(t *testing.T) {
		t.Parallel()

		handler, s := newServer(t)
		s.categories.createFn = func(context.Context, domain.NewCategory) (domain.Category, error) {
			return domain.Category{}, domain.ErrCategoryAlreadyExists
		}

		rec := do(t, handler, request{method: "POST", path: "/api/v1/categories", body: `{"name":"Продукты"}`})

		require.Equal(t, http.StatusConflict, rec.Code)
		assert.Equal(t, "category_already_exists", decodeError(t, rec).Code)
	})

	t.Run("чужая категория -> 422", func(t *testing.T) {
		t.Parallel()

		handler, s := newServer(t)
		s.operations.createFn = func(context.Context, domain.NewOperation) (domain.Operation, error) {
			return domain.Operation{}, domain.ErrCategoryNotFound
		}

		rec := do(t, handler, request{method: "POST", path: "/api/v1/operations",
			body: `{"category_id":42,"type":"expense","amount":100}`})

		// 404 был бы неточен: сам ресурс /operations существует,
		// некорректна ссылка внутри тела запроса.
		require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
		body := decodeError(t, rec)
		assert.Equal(t, "category_not_found", body.Code)
		require.Len(t, body.Details, 1)
		assert.Equal(t, "category_id", body.Details[0].Field)
	})

	t.Run("ошибки валидации -> 422 с перечнем полей", func(t *testing.T) {
		t.Parallel()

		handler, s := newServer(t)
		s.operations.createFn = func(context.Context, domain.NewOperation) (domain.Operation, error) {
			return domain.Operation{}, domain.ValidationErrors{
				domain.NewValidationError("amount", "must be positive"),
				domain.NewValidationError("type", "must be one of [income expense]"),
			}
		}

		rec := do(t, handler, request{method: "POST", path: "/api/v1/operations",
			body: `{"category_id":1,"type":"transfer","amount":-1}`})

		require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
		body := decodeError(t, rec)
		assert.Equal(t, "validation_error", body.Code)
		require.Len(t, body.Details, 2)
		assert.Equal(t, "amount", body.Details[0].Field)
		assert.Equal(t, "type", body.Details[1].Field)
	})

	t.Run("неожиданная ошибка -> 500 без деталей", func(t *testing.T) {
		t.Parallel()

		handler, s := newServer(t)
		s.categories.listFn = func(context.Context, domain.UserID) ([]domain.Category, error) {
			return nil, errors.New(`pq: password authentication failed for user "finance"`)
		}

		rec := do(t, handler, request{method: "GET", path: "/api/v1/categories"})

		require.Equal(t, http.StatusInternalServerError, rec.Code)
		body := decodeError(t, rec)
		assert.Equal(t, "internal_error", body.Code)
		// Детали внутренней ошибки не должны утекать клиенту.
		assert.NotContains(t, rec.Body.String(), "password")
		assert.NotContains(t, rec.Body.String(), "finance")
	})
}

// ------------------------------------------------------- успешные ответы ----

func TestCreateCategory(t *testing.T) {
	t.Parallel()

	handler, s := newServer(t)
	rec := do(t, handler, request{method: "POST", path: "/api/v1/categories", body: `{"name":"Продукты"}`})

	require.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, testUser, s.categories.gotIn.UserID, "user_id должен браться из заголовка")

	var got map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, float64(1), got["id"])
	assert.Equal(t, "Продукты", got["name"])
	assert.Equal(t, "2026-07-20T12:00:00Z", got["created_at"])
}

func TestListCategoriesReturnsEmptyArrayNotNull(t *testing.T) {
	t.Parallel()

	handler, s := newServer(t)
	s.categories.listFn = func(context.Context, domain.UserID) ([]domain.Category, error) {
		return nil, nil
	}

	rec := do(t, handler, request{method: "GET", path: "/api/v1/categories"})

	require.Equal(t, http.StatusOK, rec.Code)
	// nil-срез сериализовался бы в null и сломал бы клиентов, ожидающих массив.
	assert.JSONEq(t, `{"categories":[]}`, rec.Body.String())
}

func TestCreateOperationFullPayload(t *testing.T) {
	t.Parallel()

	handler, s := newServer(t)
	body := `{"category_id":1,"type":"expense","amount":150000,` +
		`"occurred_at":"2026-07-15T18:32:00Z","comment":"Пятёрочка"}`

	rec := do(t, handler, request{method: "POST", path: "/api/v1/operations", body: body})

	require.Equal(t, http.StatusCreated, rec.Code)

	require.NotNil(t, s.operations.gotIn.OccurredAt)
	assert.Equal(t, time.Date(2026, time.July, 15, 18, 32, 0, 0, time.UTC), *s.operations.gotIn.OccurredAt)
	assert.Equal(t, domain.OperationExpense, s.operations.gotIn.Type)
	assert.Equal(t, int64(150000), s.operations.gotIn.Amount)

	assert.JSONEq(t, `{
		"id": 101,
		"type": "expense",
		"amount": 150000,
		"category": {"id": 1, "name": "Продукты"},
		"comment": "Пятёрочка",
		"occurred_at": "2026-07-15T18:32:00Z",
		"created_at": "2026-07-20T12:00:00Z"
	}`, rec.Body.String())
}

// Отсутствующий occurred_at доходит до сервиса как nil: подстановка текущего
// времени - решение сервисного слоя, а не транспортного.
func TestCreateOperationWithoutOccurredAtPassesNil(t *testing.T) {
	t.Parallel()

	handler, s := newServer(t)
	rec := do(t, handler, request{method: "POST", path: "/api/v1/operations",
		body: `{"category_id":1,"type":"income","amount":2000000}`})

	require.Equal(t, http.StatusCreated, rec.Code)
	assert.Nil(t, s.operations.gotIn.OccurredAt)
}

// Явно переданное нулевое время должно дойти до валидации, а не быть принято
// за отсутствующее значение.
func TestCreateOperationExplicitZeroTimeReachesService(t *testing.T) {
	t.Parallel()

	handler, s := newServer(t)
	rec := do(t, handler, request{method: "POST", path: "/api/v1/operations",
		body: `{"category_id":1,"type":"expense","amount":100,"occurred_at":"0001-01-01T00:00:00Z"}`})

	require.Equal(t, http.StatusCreated, rec.Code, "заглушка сервиса не валидирует; важен сам факт передачи")
	require.NotNil(t, s.operations.gotIn.OccurredAt, "нулевое время не должно превращаться в nil")
	assert.True(t, s.operations.gotIn.OccurredAt.IsZero())
}

func TestListOperationsPassesFiltersToService(t *testing.T) {
	t.Parallel()

	handler, s := newServer(t)
	rec := do(t, handler, request{method: "GET",
		path: "/api/v1/operations?from=2026-07-01&to=2026-07-31&type=expense&category_id=3"})

	require.Equal(t, http.StatusOK, rec.Code)

	filter := s.operations.gotFilter
	assert.Equal(t, testUser, filter.UserID)
	require.NotNil(t, filter.From)
	require.NotNil(t, filter.To)
	require.NotNil(t, filter.Type)
	require.NotNil(t, filter.CategoryID)

	assert.Equal(t, time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC), *filter.From)
	// Верхняя граница развёрнута до конца суток.
	assert.Equal(t, time.Date(2026, time.July, 31, 23, 59, 59, 999999000, time.UTC), *filter.To)
	assert.Equal(t, domain.OperationExpense, *filter.Type)
	assert.Equal(t, int64(3), *filter.CategoryID)
}

func TestListOperationsWithoutFiltersPassesNils(t *testing.T) {
	t.Parallel()

	handler, s := newServer(t)
	rec := do(t, handler, request{method: "GET", path: "/api/v1/operations"})

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Nil(t, s.operations.gotFilter.From)
	assert.Nil(t, s.operations.gotFilter.To)
	assert.Nil(t, s.operations.gotFilter.Type)
	assert.Nil(t, s.operations.gotFilter.CategoryID)
}

// Форма ответа статистики сверяется с примером из технического задания.
func TestStatsResponseShape(t *testing.T) {
	t.Parallel()

	handler, s := newServer(t)
	s.stats.getFn = func(_ context.Context, _ domain.UserID, period domain.Period) (domain.Stats, error) {
		return domain.Stats{
			Period:       period,
			TotalIncome:  20000000,
			TotalExpense: 6500000,
			Balance:      13500000,
			ExpensesByCategory: []domain.CategoryExpense{
				{CategoryID: 1, Category: "Продукты", Amount: 3200000},
				{CategoryID: 2, Category: "Аренда", Amount: 2500000},
				{CategoryID: 3, Category: "Подписки", Amount: 800000},
			},
		}, nil
	}

	rec := do(t, handler, request{method: "GET", path: "/api/v1/stats?from=2026-07-01&to=2026-07-31"})

	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{
		"period": {"from": "2026-07-01T00:00:00Z", "to": "2026-07-31T23:59:59.999999Z"},
		"total_income": 20000000,
		"total_expense": 6500000,
		"balance": 13500000,
		"expenses_by_category": [
			{"category_id": 1, "category": "Продукты", "amount": 3200000},
			{"category_id": 2, "category": "Аренда", "amount": 2500000},
			{"category_id": 3, "category": "Подписки", "amount": 800000}
		]
	}`, rec.Body.String())
}

func TestStatsEmptyPeriodReturnsZerosAndEmptyArray(t *testing.T) {
	t.Parallel()

	handler, _ := newServer(t)
	rec := do(t, handler, request{method: "GET", path: "/api/v1/stats?from=2026-01-01&to=2026-01-31"})

	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{
		"period": {"from": "2026-01-01T00:00:00Z", "to": "2026-01-31T23:59:59.999999Z"},
		"total_income": 0,
		"total_expense": 0,
		"balance": 0,
		"expenses_by_category": []
	}`, rec.Body.String())
}

// Отрицательный баланс - штатный результат, а не ошибка.
func TestStatsNegativeBalance(t *testing.T) {
	t.Parallel()

	handler, s := newServer(t)
	s.stats.getFn = func(_ context.Context, _ domain.UserID, period domain.Period) (domain.Stats, error) {
		return domain.Stats{Period: period, TotalIncome: 100, TotalExpense: 500, Balance: -400}, nil
	}

	rec := do(t, handler, request{method: "GET", path: "/api/v1/stats?from=2026-07-01&to=2026-07-31"})

	require.Equal(t, http.StatusOK, rec.Code)
	var got map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, float64(-400), got["balance"])
}

func TestHealthz(t *testing.T) {
	t.Parallel()

	handler, _ := newServer(t)
	// Проба живости не требует идентификатора пользователя.
	rec := do(t, handler, request{method: "GET", path: "/healthz", userID: "-"})

	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"status":"ok"}`, rec.Body.String())
}

// Тело запроса ограничено по размеру, иначе один клиент мог бы занять всю
// доступную сервису память.
func TestRequestBodyTooLarge(t *testing.T) {
	t.Parallel()

	handler, _ := newServer(t)
	huge := `{"name":"` + strings.Repeat("я", 1<<20) + `"}`

	rec := do(t, handler, request{method: "POST", path: "/api/v1/categories", body: huge})

	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	assert.Equal(t, "request_too_large", decodeError(t, rec).Code)
}

// Паника в обработчике не должна ронять процесс.
func TestPanicRecovered(t *testing.T) {
	t.Parallel()

	handler, s := newServer(t)
	s.categories.listFn = func(context.Context, domain.UserID) ([]domain.Category, error) {
		panic("boom")
	}

	require.NotPanics(t, func() {
		rec := do(t, handler, request{method: "GET", path: "/api/v1/categories"})
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})
}

// Ответы всех эндпоинтов должны быть валидным JSON с корректным Content-Type.
func TestAllResponsesAreJSON(t *testing.T) {
	t.Parallel()

	paths := []request{
		{method: "GET", path: "/healthz", userID: "-"},
		{method: "GET", path: "/api/v1/categories"},
		{method: "GET", path: "/api/v1/operations"},
		{method: "GET", path: "/api/v1/stats?from=2026-07-01&to=2026-07-31"},
		{method: "POST", path: "/api/v1/categories", body: `{"name":"Продукты"}`},
		{method: "GET", path: "/api/v1/unknown"},
	}

	for _, req := range paths {
		t.Run(req.method+" "+req.path, func(t *testing.T) {
			t.Parallel()

			handler, _ := newServer(t)
			rec := do(t, handler, req)

			assert.Equal(t, "application/json; charset=utf-8", rec.Header().Get("Content-Type"))
			assert.True(t, json.Valid(rec.Body.Bytes()), "тело должно быть валидным JSON: %s", rec.Body.String())
		})
	}
}

// Кириллица не должна экранироваться в \uXXXX: ответ читается человеком
// как есть, в том числе в примерах README.
func TestCyrillicNotEscaped(t *testing.T) {
	t.Parallel()

	handler, _ := newServer(t)
	rec := do(t, handler, request{method: "POST", path: "/api/v1/categories", body: `{"name":"Продукты"}`})

	require.Equal(t, http.StatusCreated, rec.Code)
	assert.True(t, bytes.Contains(rec.Body.Bytes(), []byte("Продукты")),
		"кириллица должна оставаться читаемой, получено: %s", rec.Body.String())
}
