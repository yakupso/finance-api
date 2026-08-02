package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"finance-api/internal/domain"
	"finance-api/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ------------------------------------------------------------- категории ----

func TestCategoryServiceCreateNormalizesName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"обрезает пробелы", "  Продукты  ", "Продукты"},
		{"схлопывает внутренние пробелы", "Кафе   и   рестораны", "Кафе и рестораны"},
		{"убирает переводы строк", "Аренда\n", "Аренда"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := &fakeCategoryRepo{}
			svc := service.NewCategoryService(repo)

			got, err := svc.Create(ctx(t), domain.NewCategory{UserID: testUser, Name: tt.input})
			require.NoError(t, err)

			// В хранилище должно уйти уже нормализованное имя: иначе в БД
			// попали бы «Продукты» и «Продукты » как разные категории.
			assert.Equal(t, tt.want, repo.createGot.Name, "имя должно нормализоваться до записи")
			assert.Equal(t, tt.want, got.Name)
		})
	}
}

func TestCategoryServiceCreateRejectsBlankName(t *testing.T) {
	t.Parallel()

	repo := &fakeCategoryRepo{}
	svc := service.NewCategoryService(repo)

	_, err := svc.Create(ctx(t), domain.NewCategory{UserID: testUser, Name: "   \t  "})

	require.Error(t, err)
	validationErrs, ok := domain.AsValidationErrors(err)
	require.True(t, ok)
	assert.Equal(t, "name", validationErrs[0].Field)
	assert.Zero(t, repo.createCnt, "невалидные данные не должны доходить до хранилища")
}

func TestCategoryServiceCreatePropagatesConflict(t *testing.T) {
	t.Parallel()

	repo := &fakeCategoryRepo{
		createFn: func(context.Context, domain.NewCategory) (domain.Category, error) {
			return domain.Category{}, domain.ErrCategoryAlreadyExists
		},
	}
	svc := service.NewCategoryService(repo)

	_, err := svc.Create(ctx(t), domain.NewCategory{UserID: testUser, Name: "Продукты"})

	// Оборачивание не должно ломать errors.Is: транспорт опознаёт ошибку именно так.
	require.ErrorIs(t, err, domain.ErrCategoryAlreadyExists)
}

func TestCategoryServiceList(t *testing.T) {
	t.Parallel()

	want := []domain.Category{{ID: 1, Name: "Аренда"}, {ID: 2, Name: "Продукты"}}
	repo := &fakeCategoryRepo{
		listFn: func(_ context.Context, userID domain.UserID) ([]domain.Category, error) {
			assert.Equal(t, testUser, userID)
			return want, nil
		},
	}
	svc := service.NewCategoryService(repo)

	got, err := svc.List(ctx(t), testUser)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestCategoryServiceListPropagatesError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("connection refused")
	repo := &fakeCategoryRepo{
		listFn: func(context.Context, domain.UserID) ([]domain.Category, error) {
			return nil, sentinel
		},
	}
	svc := service.NewCategoryService(repo)

	_, err := svc.List(ctx(t), testUser)
	require.ErrorIs(t, err, sentinel)
}

// -------------------------------------------------------------- операции ----

func newOperationSvc(repo *fakeOperationRepo) *service.OperationService {
	return service.NewOperationService(repo, service.WithClock(func() time.Time { return fixedNow }))
}

func TestOperationServiceCreateDefaultsOccurredAtToNow(t *testing.T) {
	t.Parallel()

	repo := &fakeOperationRepo{}
	svc := newOperationSvc(repo)

	_, err := svc.Create(ctx(t), domain.NewOperation{
		UserID: testUser, CategoryID: 1, Type: domain.OperationExpense, Amount: 150000,
	})
	require.NoError(t, err)

	require.NotNil(t, repo.createGot.OccurredAt)
	assert.Equal(t, fixedNow, *repo.createGot.OccurredAt,
		"при отсутствии occurred_at подставляется текущее время")
}

func TestOperationServiceCreateKeepsExplicitOccurredAt(t *testing.T) {
	t.Parallel()

	repo := &fakeOperationRepo{}
	svc := newOperationSvc(repo)
	explicit := time.Date(2026, time.July, 15, 18, 32, 0, 0, time.UTC)

	_, err := svc.Create(ctx(t), domain.NewOperation{
		UserID: testUser, CategoryID: 1, Type: domain.OperationExpense,
		Amount: 150000, OccurredAt: &explicit,
	})
	require.NoError(t, err)

	require.NotNil(t, repo.createGot.OccurredAt)
	assert.Equal(t, explicit, *repo.createGot.OccurredAt)
}

// Регрессия: строка "0001-01-01T00:00:00Z" разбирается ровно в нулевое
// time.Time. Пока «не указано» определялось через IsZero(), такая дата
// подменялась текущим временем и операция создавалась вместо отказа.
func TestOperationServiceCreateRejectsExplicitZeroTime(t *testing.T) {
	t.Parallel()

	repo := &fakeOperationRepo{}
	svc := newOperationSvc(repo)

	_, err := svc.Create(ctx(t), domain.NewOperation{
		UserID: testUser, CategoryID: 1, Type: domain.OperationExpense,
		Amount: 150000, OccurredAt: ptr(time.Time{}),
	})

	require.Error(t, err)
	validationErrs, ok := domain.AsValidationErrors(err)
	require.True(t, ok)
	assert.Equal(t, "occurred_at", validationErrs[0].Field)
	assert.Zero(t, repo.createCnt, "операция не должна дойти до хранилища")
}

func TestOperationServiceCreateNormalizesComment(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input *string
		want  *string
	}{
		{"обрезает пробелы", ptr("  Пятёрочка  "), ptr("Пятёрочка")},
		{"пустая строка становится nil", ptr(""), nil},
		{"строка из пробелов становится nil", ptr("   "), nil},
		{"nil остаётся nil", nil, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := &fakeOperationRepo{}
			svc := newOperationSvc(repo)

			_, err := svc.Create(ctx(t), domain.NewOperation{
				UserID: testUser, CategoryID: 1, Type: domain.OperationExpense,
				Amount: 150000, Comment: tt.input,
			})
			require.NoError(t, err)

			if tt.want == nil {
				assert.Nil(t, repo.createGot.Comment, "в БД не должно попадать пустых комментариев")
				return
			}
			require.NotNil(t, repo.createGot.Comment)
			assert.Equal(t, *tt.want, *repo.createGot.Comment)
		})
	}
}

func TestOperationServiceCreateValidatesFutureDateAgainstInjectedClock(t *testing.T) {
	t.Parallel()

	repo := &fakeOperationRepo{}
	svc := newOperationSvc(repo)

	// На день дальше допустимой границы относительно подменённого «сейчас».
	tooFar := fixedNow.Add(domain.MaxOccurredAtAhead + 24*time.Hour)

	_, err := svc.Create(ctx(t), domain.NewOperation{
		UserID: testUser, CategoryID: 1, Type: domain.OperationExpense,
		Amount: 150000, OccurredAt: &tooFar,
	})

	require.Error(t, err)
	assert.Zero(t, repo.createCnt)
}

func TestOperationServiceCreatePropagatesCategoryNotFound(t *testing.T) {
	t.Parallel()

	repo := &fakeOperationRepo{
		createFn: func(context.Context, domain.NewOperation) (domain.Operation, error) {
			return domain.Operation{}, domain.ErrCategoryNotFound
		},
	}
	svc := newOperationSvc(repo)

	_, err := svc.Create(ctx(t), domain.NewOperation{
		UserID: testUser, CategoryID: 42, Type: domain.OperationExpense, Amount: 100,
	})

	require.ErrorIs(t, err, domain.ErrCategoryNotFound)
}

func TestOperationServiceListPassesFilterThrough(t *testing.T) {
	t.Parallel()

	repo := &fakeOperationRepo{}
	svc := newOperationSvc(repo)

	from := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, time.July, 31, 23, 59, 59, 0, time.UTC)
	opType := domain.OperationExpense
	categoryID := int64(7)

	want := domain.OperationFilter{
		UserID: testUser, From: &from, To: &to, Type: &opType, CategoryID: &categoryID,
	}

	_, err := svc.List(ctx(t), want)
	require.NoError(t, err)
	assert.Equal(t, want, repo.listGot, "фильтр должен доходить до хранилища без изменений")
}

func TestOperationServiceListReturnsEmptySliceNotError(t *testing.T) {
	t.Parallel()

	repo := &fakeOperationRepo{
		listFn: func(context.Context, domain.OperationFilter) ([]domain.Operation, error) {
			return []domain.Operation{}, nil
		},
	}
	svc := newOperationSvc(repo)

	got, err := svc.List(ctx(t), domain.OperationFilter{UserID: testUser})
	require.NoError(t, err)
	assert.Empty(t, got)
}

// ------------------------------------------------------------ статистика ----

func TestStatsServiceGetPassesPeriodThrough(t *testing.T) {
	t.Parallel()

	repo := &fakeStatsRepo{}
	svc := service.NewStatsService(repo)

	period := domain.Period{
		From: time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2026, time.July, 31, 23, 59, 59, 999999000, time.UTC),
	}

	_, err := svc.Get(ctx(t), testUser, period)
	require.NoError(t, err)

	assert.Equal(t, testUser, repo.gotUserID)
	assert.Equal(t, period, repo.gotPeriod)
}

// Сервис не должен ничего досчитывать поверх результата PostgreSQL:
// вся агрегация выполняется в БД, а balance приходит из запроса.
func TestStatsServiceReturnsRepositoryResultUnchanged(t *testing.T) {
	t.Parallel()

	want := domain.Stats{
		TotalIncome:  20000000,
		TotalExpense: 6500000,
		Balance:      13500000,
		ExpensesByCategory: []domain.CategoryExpense{
			{CategoryID: 1, Category: "Продукты", Amount: 3200000},
		},
	}
	repo := &fakeStatsRepo{
		getFn: func(_ context.Context, _ domain.UserID, period domain.Period) (domain.Stats, error) {
			want.Period = period
			return want, nil
		},
	}
	svc := service.NewStatsService(repo)

	got, err := svc.Get(ctx(t), testUser, domain.Period{})
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestStatsServiceGetPropagatesError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("query timeout")
	repo := &fakeStatsRepo{
		getFn: func(context.Context, domain.UserID, domain.Period) (domain.Stats, error) {
			return domain.Stats{}, sentinel
		},
	}
	svc := service.NewStatsService(repo)

	_, err := svc.Get(ctx(t), testUser, domain.Period{})
	require.ErrorIs(t, err, sentinel)
}
