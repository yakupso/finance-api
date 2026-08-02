package domain_test

import (
	"strings"
	"testing"
	"time"

	"finance-api/internal/domain"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	testUser = uuid.MustParse("11111111-1111-4111-8111-111111111111")
	testNow  = time.Date(2026, time.July, 20, 12, 0, 0, 0, time.UTC)
)

func ptr[T any](v T) *T { return &v }

func TestNormalizeCategoryName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"убирает окружающие пробелы", "  Продукты  ", "Продукты"},
		{"схлопывает внутренние пробелы", "Продукты   и  напитки", "Продукты и напитки"},
		{"убирает табуляции и переводы строк", "\tПродукты\n", "Продукты"},
		{"строка из пробелов становится пустой", "   \t\n ", ""},
		{"нормализованное имя не меняется", "Продукты", "Продукты"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, domain.NormalizeCategoryName(tt.in))
		})
	}
}

func TestNewCategoryValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		in        domain.NewCategory
		wantField string // пусто - ошибок быть не должно
	}{
		{
			name: "корректная категория",
			in:   domain.NewCategory{UserID: testUser, Name: "Продукты"},
		},
		{
			name: "имя на границе длины",
			in:   domain.NewCategory{UserID: testUser, Name: strings.Repeat("я", domain.MaxCategoryNameLen)},
		},
		{
			name:      "пустое имя",
			in:        domain.NewCategory{UserID: testUser, Name: ""},
			wantField: "name",
		},
		{
			name:      "имя длиннее предела",
			in:        domain.NewCategory{UserID: testUser, Name: strings.Repeat("я", domain.MaxCategoryNameLen+1)},
			wantField: "name",
		},
		{
			name:      "отсутствует пользователь",
			in:        domain.NewCategory{Name: "Продукты"},
			wantField: "user_id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.in.Validate()
			if tt.wantField == "" {
				require.NoError(t, err)
				return
			}

			require.Error(t, err)
			assertHasFieldError(t, err, tt.wantField)
		})
	}
}

// Длина имени считается в символах, а не в байтах: кириллическое имя из 100
// символов занимает 200 байт и не должно отвергаться.
func TestCategoryNameLengthCountedInRunes(t *testing.T) {
	t.Parallel()

	name := strings.Repeat("я", domain.MaxCategoryNameLen)
	require.Greater(t, len(name), domain.MaxCategoryNameLen, "имя должно быть длиннее в байтах")

	in := domain.NewCategory{UserID: testUser, Name: name}
	assert.NoError(t, in.Validate())
}

func TestNewOperationValidate(t *testing.T) {
	t.Parallel()

	valid := func(mutate func(*domain.NewOperation)) domain.NewOperation {
		occurredAt := testNow.Add(-24 * time.Hour)
		in := domain.NewOperation{
			UserID:     testUser,
			CategoryID: 1,
			Type:       domain.OperationExpense,
			Amount:     150000,
			OccurredAt: &occurredAt,
		}
		if mutate != nil {
			mutate(&in)
		}
		return in
	}

	tests := []struct {
		name       string
		in         domain.NewOperation
		wantFields []string
	}{
		{
			name: "корректная операция",
			in:   valid(nil),
		},
		{
			name: "минимальная допустимая сумма",
			in:   valid(func(o *domain.NewOperation) { o.Amount = domain.MinAmount }),
		},
		{
			name: "максимальная допустимая сумма",
			in:   valid(func(o *domain.NewOperation) { o.Amount = domain.MaxAmount }),
		},
		{
			name: "дата ровно на нижней границе",
			in:   valid(func(o *domain.NewOperation) { o.OccurredAt = ptr(domain.MinOccurredAt) }),
		},
		{
			name: "дата на верхней границе будущего",
			in: valid(func(o *domain.NewOperation) {
				o.OccurredAt = ptr(testNow.Add(domain.MaxOccurredAtAhead))
			}),
		},
		{
			name:       "нулевая сумма",
			in:         valid(func(o *domain.NewOperation) { o.Amount = 0 }),
			wantFields: []string{"amount"},
		},
		{
			name:       "отрицательная сумма",
			in:         valid(func(o *domain.NewOperation) { o.Amount = -1 }),
			wantFields: []string{"amount"},
		},
		{
			name:       "сумма выше предела",
			in:         valid(func(o *domain.NewOperation) { o.Amount = domain.MaxAmount + 1 }),
			wantFields: []string{"amount"},
		},
		{
			name:       "неизвестный тип операции",
			in:         valid(func(o *domain.NewOperation) { o.Type = "transfer" }),
			wantFields: []string{"type"},
		},
		{
			name:       "пустой тип операции",
			in:         valid(func(o *domain.NewOperation) { o.Type = "" }),
			wantFields: []string{"type"},
		},
		{
			name:       "нулевая категория",
			in:         valid(func(o *domain.NewOperation) { o.CategoryID = 0 }),
			wantFields: []string{"category_id"},
		},
		{
			name:       "отрицательная категория",
			in:         valid(func(o *domain.NewOperation) { o.CategoryID = -5 }),
			wantFields: []string{"category_id"},
		},
		{
			name:       "дата не указана",
			in:         valid(func(o *domain.NewOperation) { o.OccurredAt = nil }),
			wantFields: []string{"occurred_at"},
		},
		{
			// Регрессия: строка "0001-01-01T00:00:00Z" разбирается ровно
			// в нулевое time.Time. Пока признаком «не указано» служил IsZero(),
			// такая дата молча подменялась текущим временем вместо отказа.
			name:       "нулевое время как явно переданная дата",
			in:         valid(func(o *domain.NewOperation) { o.OccurredAt = ptr(time.Time{}) }),
			wantFields: []string{"occurred_at"},
		},
		{
			name: "дата раньше нижней границы",
			in: valid(func(o *domain.NewOperation) {
				o.OccurredAt = ptr(domain.MinOccurredAt.Add(-time.Second))
			}),
			wantFields: []string{"occurred_at"},
		},
		{
			name: "дата слишком далеко в будущем",
			in: valid(func(o *domain.NewOperation) {
				o.OccurredAt = ptr(testNow.Add(domain.MaxOccurredAtAhead + time.Second))
			}),
			wantFields: []string{"occurred_at"},
		},
		{
			name: "комментарий длиннее предела",
			in: valid(func(o *domain.NewOperation) {
				o.Comment = ptr(strings.Repeat("я", domain.MaxCommentLen+1))
			}),
			wantFields: []string{"comment"},
		},
		{
			name: "комментарий на границе длины",
			in: valid(func(o *domain.NewOperation) {
				o.Comment = ptr(strings.Repeat("я", domain.MaxCommentLen))
			}),
		},
		{
			// Клиент должен узнать обо всех проблемах за один запрос.
			name: "несколько нарушений возвращаются разом",
			in: valid(func(o *domain.NewOperation) {
				o.CategoryID = 0
				o.Type = "transfer"
				o.Amount = -10
			}),
			wantFields: []string{"category_id", "type", "amount"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.in.Validate(testNow)
			if len(tt.wantFields) == 0 {
				require.NoError(t, err)
				return
			}

			require.Error(t, err)
			validationErrs, ok := domain.AsValidationErrors(err)
			require.True(t, ok, "ошибка должна быть распознана как ValidationErrors")
			require.Len(t, validationErrs, len(tt.wantFields))

			for _, field := range tt.wantFields {
				assertHasFieldError(t, err, field)
			}
		})
	}
}

func TestOperationTypeIsValid(t *testing.T) {
	t.Parallel()

	assert.True(t, domain.OperationIncome.IsValid())
	assert.True(t, domain.OperationExpense.IsValid())
	assert.False(t, domain.OperationType("").IsValid())
	assert.False(t, domain.OperationType("Income").IsValid(), "тип чувствителен к регистру")
	assert.False(t, domain.OperationType("transfer").IsValid())
}

// AsValidationErrors должна распознавать как набор ошибок, так и одиночную.
func TestAsValidationErrors(t *testing.T) {
	t.Parallel()

	t.Run("одиночная ошибка", func(t *testing.T) {
		t.Parallel()
		errs, ok := domain.AsValidationErrors(domain.NewValidationError("amount", "must be positive"))
		require.True(t, ok)
		require.Len(t, errs, 1)
		assert.Equal(t, "amount", errs[0].Field)
	})

	t.Run("обычная ошибка не распознаётся", func(t *testing.T) {
		t.Parallel()
		_, ok := domain.AsValidationErrors(domain.ErrCategoryNotFound)
		assert.False(t, ok)
	})

	t.Run("nil не распознаётся", func(t *testing.T) {
		t.Parallel()
		_, ok := domain.AsValidationErrors(nil)
		assert.False(t, ok)
	})
}

func assertHasFieldError(t *testing.T, err error, field string) {
	t.Helper()

	validationErrs, ok := domain.AsValidationErrors(err)
	require.True(t, ok, "ожидались ошибки валидации, получено: %v", err)

	for _, item := range validationErrs {
		if item.Field == field {
			assert.NotEmpty(t, item.Message, "сообщение об ошибке для поля %q не должно быть пустым", field)
			return
		}
	}
	t.Errorf("ожидалась ошибка в поле %q, получено: %v", field, err)
}
