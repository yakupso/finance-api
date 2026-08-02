//go:build integration

package postgres_test

import (
	"testing"
	"time"

	"finance-api/internal/domain"
	"finance-api/internal/repository/postgres"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	userA = uuid.MustParse("11111111-1111-4111-8111-111111111111")
	userB = uuid.MustParse("22222222-2222-4222-8222-222222222222")
)

func at(t *testing.T, value string) time.Time {
	t.Helper()

	parsed, err := time.Parse(time.RFC3339, value)
	require.NoError(t, err)
	return parsed.UTC()
}

func ptr[T any](v T) *T { return &v }

// ------------------------------------------------------------- категории ----

func TestCategoryRepositoryCreateAndList(t *testing.T) {
	truncate(t)
	ctx := testCtx(t)
	repo := postgres.NewCategoryRepository(testPool)

	created, err := repo.Create(ctx, domain.NewCategory{UserID: userA, Name: "Продукты"})
	require.NoError(t, err)
	assert.Equal(t, int64(1), created.ID)
	assert.Equal(t, userA, created.UserID)
	assert.Equal(t, "Продукты", created.Name)
	assert.False(t, created.CreatedAt.IsZero())
	assert.Equal(t, time.UTC, created.CreatedAt.Location(), "время должно приводиться к UTC")

	_, err = repo.Create(ctx, domain.NewCategory{UserID: userA, Name: "Аренда"})
	require.NoError(t, err)

	list, err := repo.List(ctx, userA)
	require.NoError(t, err)
	require.Len(t, list, 2)
	// Сортировка по названию задана в запросе.
	assert.Equal(t, "Аренда", list[0].Name)
	assert.Equal(t, "Продукты", list[1].Name)
}

// Уникальный индекс построен по lower(name), поэтому имена, различающиеся
// только регистром, считаются одной категорией.
func TestCategoryRepositoryDuplicateNameIsCaseInsensitive(t *testing.T) {
	truncate(t)
	ctx := testCtx(t)
	repo := postgres.NewCategoryRepository(testPool)

	_, err := repo.Create(ctx, domain.NewCategory{UserID: userA, Name: "Продукты"})
	require.NoError(t, err)

	for _, name := range []string{"Продукты", "продукты", "ПРОДУКТЫ", "ПрОдУкТы"} {
		_, err := repo.Create(ctx, domain.NewCategory{UserID: userA, Name: name})
		require.ErrorIs(t, err, domain.ErrCategoryAlreadyExists,
			"имя %q должно считаться дублем", name)
	}
}

// Уникальность действует в пределах пользователя, а не глобально.
func TestCategoryRepositorySameNameForDifferentUsers(t *testing.T) {
	truncate(t)
	ctx := testCtx(t)
	repo := postgres.NewCategoryRepository(testPool)

	_, err := repo.Create(ctx, domain.NewCategory{UserID: userA, Name: "Продукты"})
	require.NoError(t, err)

	_, err = repo.Create(ctx, domain.NewCategory{UserID: userB, Name: "Продукты"})
	require.NoError(t, err, "у другого пользователя такое имя должно быть допустимо")
}

func TestCategoryRepositoryListIsolatedByUser(t *testing.T) {
	truncate(t)
	ctx := testCtx(t)
	repo := postgres.NewCategoryRepository(testPool)

	_, err := repo.Create(ctx, domain.NewCategory{UserID: userA, Name: "Продукты"})
	require.NoError(t, err)
	_, err = repo.Create(ctx, domain.NewCategory{UserID: userB, Name: "Аренда"})
	require.NoError(t, err)

	listA, err := repo.List(ctx, userA)
	require.NoError(t, err)
	require.Len(t, listA, 1)
	assert.Equal(t, "Продукты", listA[0].Name)

	listB, err := repo.List(ctx, userB)
	require.NoError(t, err)
	require.Len(t, listB, 1)
	assert.Equal(t, "Аренда", listB[0].Name)
}

func TestCategoryRepositoryListEmpty(t *testing.T) {
	truncate(t)
	ctx := testCtx(t)
	repo := postgres.NewCategoryRepository(testPool)

	list, err := repo.List(ctx, userA)
	require.NoError(t, err)
	assert.Empty(t, list)
}

// -------------------------------------------------------------- операции ----

// Центральная проверка целостности: составной внешний ключ
// (category_id, user_id) не даёт привязать операцию к чужой категории.
// Обычный REFERENCES categories(id) этот случай пропустил бы.
func TestOperationRepositoryRejectsCategoryOfAnotherUser(t *testing.T) {
	truncate(t)
	ctx := testCtx(t)

	categories := postgres.NewCategoryRepository(testPool)
	operations := postgres.NewOperationRepository(testPool)

	categoryOfA, err := categories.Create(ctx, domain.NewCategory{UserID: userA, Name: "Продукты"})
	require.NoError(t, err)

	_, err = operations.Create(ctx, domain.NewOperation{
		UserID:     userB, // другой пользователь
		CategoryID: categoryOfA.ID,
		Type:       domain.OperationExpense,
		Amount:     150000,
		OccurredAt: ptr(at(t, "2026-07-15T18:32:00Z")),
	})

	require.ErrorIs(t, err, domain.ErrCategoryNotFound)

	list, err := operations.List(ctx, domain.OperationFilter{UserID: userB})
	require.NoError(t, err)
	assert.Empty(t, list, "операция не должна была сохраниться")
}

func TestOperationRepositoryRejectsUnknownCategory(t *testing.T) {
	truncate(t)
	ctx := testCtx(t)
	operations := postgres.NewOperationRepository(testPool)

	_, err := operations.Create(ctx, domain.NewOperation{
		UserID:     userA,
		CategoryID: 999999,
		Type:       domain.OperationExpense,
		Amount:     100,
		OccurredAt: ptr(at(t, "2026-07-15T18:32:00Z")),
	})

	require.ErrorIs(t, err, domain.ErrCategoryNotFound)
}

func TestOperationRepositoryCreateReturnsCategoryName(t *testing.T) {
	truncate(t)
	ctx := testCtx(t)

	categories := postgres.NewCategoryRepository(testPool)
	operations := postgres.NewOperationRepository(testPool)

	category, err := categories.Create(ctx, domain.NewCategory{UserID: userA, Name: "Продукты"})
	require.NoError(t, err)

	comment := "Пятёрочка"
	occurredAt := at(t, "2026-07-15T18:32:00Z")

	op, err := operations.Create(ctx, domain.NewOperation{
		UserID:     userA,
		CategoryID: category.ID,
		Type:       domain.OperationExpense,
		Amount:     150000,
		Comment:    &comment,
		OccurredAt: &occurredAt,
	})
	require.NoError(t, err)

	assert.Equal(t, int64(1), op.ID)
	assert.Equal(t, domain.OperationExpense, op.Type)
	assert.Equal(t, int64(150000), op.Amount)
	// Имя категории приходит одним запросом через CTE, без второго обращения к БД.
	assert.Equal(t, category.ID, op.Category.ID)
	assert.Equal(t, "Продукты", op.Category.Name)
	require.NotNil(t, op.Comment)
	assert.Equal(t, comment, *op.Comment)
	assert.True(t, occurredAt.Equal(op.OccurredAt))
	assert.Equal(t, time.UTC, op.OccurredAt.Location())
}

func TestOperationRepositoryNullComment(t *testing.T) {
	truncate(t)
	ctx := testCtx(t)

	categories := postgres.NewCategoryRepository(testPool)
	operations := postgres.NewOperationRepository(testPool)

	category, err := categories.Create(ctx, domain.NewCategory{UserID: userA, Name: "Продукты"})
	require.NoError(t, err)

	op, err := operations.Create(ctx, domain.NewOperation{
		UserID:     userA,
		CategoryID: category.ID,
		Type:       domain.OperationExpense,
		Amount:     100,
		Comment:    nil,
		OccurredAt: ptr(at(t, "2026-07-15T18:32:00Z")),
	})
	require.NoError(t, err)
	assert.Nil(t, op.Comment)
}

// CHECK-ограничения - последняя линия защиты: даже при обращении в обход
// сервиса некорректные данные в таблицу не попадут.
func TestOperationRepositoryDatabaseConstraints(t *testing.T) {
	truncate(t)
	ctx := testCtx(t)

	categories := postgres.NewCategoryRepository(testPool)
	operations := postgres.NewOperationRepository(testPool)

	category, err := categories.Create(ctx, domain.NewCategory{UserID: userA, Name: "Продукты"})
	require.NoError(t, err)

	base := func() domain.NewOperation {
		return domain.NewOperation{
			UserID:     userA,
			CategoryID: category.ID,
			Type:       domain.OperationExpense,
			Amount:     100,
			OccurredAt: ptr(at(t, "2026-07-15T18:32:00Z")),
		}
	}

	tests := []struct {
		name   string
		mutate func(*domain.NewOperation)
	}{
		{"нулевая сумма", func(o *domain.NewOperation) { o.Amount = 0 }},
		{"отрицательная сумма", func(o *domain.NewOperation) { o.Amount = -100 }},
		{"дата раньше 2000 года", func(o *domain.NewOperation) {
			o.OccurredAt = ptr(at(t, "1999-12-31T23:59:59Z"))
		}},
		{"нулевое время", func(o *domain.NewOperation) { o.OccurredAt = ptr(time.Time{}) }},
		{"неизвестный тип операции", func(o *domain.NewOperation) { o.Type = "transfer" }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := base()
			tt.mutate(&in)

			_, err := operations.Create(ctx, in)
			require.Error(t, err, "БД должна отвергнуть запись")
		})
	}
}

// ----------------------------------------------------- фильтрация списка ----

// seedOperations готовит фиксированный набор данных для тестов фильтрации
// и статистики. Возвращает идентификаторы категорий по названию.
func seedOperations(t *testing.T) map[string]int64 {
	t.Helper()

	ctx := testCtx(t)
	categories := postgres.NewCategoryRepository(testPool)
	operations := postgres.NewOperationRepository(testPool)

	ids := make(map[string]int64)
	for _, name := range []string{"Зарплата", "Продукты", "Аренда", "Подписки"} {
		category, err := categories.Create(ctx, domain.NewCategory{UserID: userA, Name: name})
		require.NoError(t, err)
		ids[name] = category.ID
	}

	seed := []struct {
		category   string
		opType     domain.OperationType
		amount     int64
		occurredAt string
	}{
		// Июль: доход 20 000 00, расходы 3 200 000 + 2 500 000 + 800 000.
		{"Зарплата", domain.OperationIncome, 20000000, "2026-07-01T09:00:00Z"},
		{"Продукты", domain.OperationExpense, 1800000, "2026-07-05T18:20:00Z"},
		{"Аренда", domain.OperationExpense, 2500000, "2026-07-10T12:00:00Z"},
		{"Продукты", domain.OperationExpense, 1400000, "2026-07-15T18:32:00Z"},
		// Ровно на верхней границе июля - должна попадать в период.
		{"Подписки", domain.OperationExpense, 800000, "2026-07-31T23:59:59Z"},
		// Первая секунда августа - не должна попадать.
		{"Продукты", domain.OperationExpense, 950000, "2026-08-01T00:00:00Z"},
	}

	for _, s := range seed {
		_, err := operations.Create(ctx, domain.NewOperation{
			UserID:     userA,
			CategoryID: ids[s.category],
			Type:       s.opType,
			Amount:     s.amount,
			OccurredAt: ptr(at(t, s.occurredAt)),
		})
		require.NoError(t, err)
	}

	// Данные другого пользователя: не должны попадать ни в списки, ни в статистику.
	categoryB, err := categories.Create(ctx, domain.NewCategory{UserID: userB, Name: "Прочее"})
	require.NoError(t, err)
	_, err = operations.Create(ctx, domain.NewOperation{
		UserID:     userB,
		CategoryID: categoryB.ID,
		Type:       domain.OperationExpense,
		Amount:     777777,
		OccurredAt: ptr(at(t, "2026-07-15T00:00:00Z")),
	})
	require.NoError(t, err)

	return ids
}

func TestOperationRepositoryListFilters(t *testing.T) {
	truncate(t)
	ids := seedOperations(t)
	ctx := testCtx(t)
	repo := postgres.NewOperationRepository(testPool)

	july := struct{ from, to time.Time }{
		from: at(t, "2026-07-01T00:00:00Z"),
		to:   at(t, "2026-07-31T23:59:59.999999Z"),
	}

	tests := []struct {
		name       string
		filter     domain.OperationFilter
		wantCount  int
		wantAmount int64 // 0 - сумму не проверяем
	}{
		{
			name:      "без фильтров - все операции пользователя",
			filter:    domain.OperationFilter{UserID: userA},
			wantCount: 6,
		},
		{
			name:      "период: июль",
			filter:    domain.OperationFilter{UserID: userA, From: &july.from, To: &july.to},
			wantCount: 5,
		},
		{
			name:      "только доходы",
			filter:    domain.OperationFilter{UserID: userA, Type: ptr(domain.OperationIncome)},
			wantCount: 1,
		},
		{
			name:      "только расходы",
			filter:    domain.OperationFilter{UserID: userA, Type: ptr(domain.OperationExpense)},
			wantCount: 5,
		},
		{
			name: "период и тип вместе",
			filter: domain.OperationFilter{
				UserID: userA, From: &july.from, To: &july.to,
				Type: ptr(domain.OperationExpense),
			},
			wantCount: 4,
		},
		{
			name: "фильтр по категории",
			filter: domain.OperationFilter{
				UserID: userA, CategoryID: ptr(ids["Продукты"]),
			},
			wantCount: 3,
		},
		{
			name: "все фильтры сразу",
			filter: domain.OperationFilter{
				UserID: userA, From: &july.from, To: &july.to,
				Type: ptr(domain.OperationExpense), CategoryID: ptr(ids["Продукты"]),
			},
			wantCount: 2,
		},
		{
			name: "только нижняя граница",
			filter: domain.OperationFilter{
				UserID: userA, From: ptr(at(t, "2026-08-01T00:00:00Z")),
			},
			wantCount: 1,
		},
		{
			name: "только верхняя граница",
			filter: domain.OperationFilter{
				UserID: userA, To: ptr(at(t, "2026-07-01T09:00:00Z")),
			},
			wantCount: 1,
		},
		{
			name: "период без операций",
			filter: domain.OperationFilter{
				UserID: userA,
				From:   ptr(at(t, "2020-01-01T00:00:00Z")),
				To:     ptr(at(t, "2020-12-31T23:59:59Z")),
			},
			wantCount: 0,
		},
		{
			name:      "другой пользователь видит только своё",
			filter:    domain.OperationFilter{UserID: userB},
			wantCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := repo.List(ctx, tt.filter)
			require.NoError(t, err)
			assert.Len(t, got, tt.wantCount)
		})
	}
}

// Обе границы периода включающие - проверяется поштучно, так как это
// единственное место, где ТЗ допускает двоякое толкование.
func TestOperationRepositoryPeriodBoundsAreInclusive(t *testing.T) {
	truncate(t)
	seedOperations(t)
	ctx := testCtx(t)
	repo := postgres.NewOperationRepository(testPool)

	tests := []struct {
		name      string
		from, to  string
		wantCount int
	}{
		{
			name: "операция ровно на нижней границе включается",
			from: "2026-07-01T09:00:00Z", to: "2026-07-01T09:00:00Z",
			wantCount: 1,
		},
		{
			name: "операция на секунду раньше нижней границы исключается",
			from: "2026-07-01T09:00:01Z", to: "2026-07-01T10:00:00Z",
			wantCount: 0,
		},
		{
			name: "операция ровно на верхней границе включается",
			from: "2026-07-31T00:00:00Z", to: "2026-07-31T23:59:59Z",
			wantCount: 1,
		},
		{
			name: "микросекунда до конца суток всё ещё включает операцию 23:59:59",
			from: "2026-07-31T00:00:00Z", to: "2026-07-31T23:59:59.999999Z",
			wantCount: 1,
		},
		{
			name: "полночь 1 августа не входит в июль",
			from: "2026-07-01T00:00:00Z", to: "2026-07-31T23:59:59.999999Z",
			wantCount: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := repo.List(ctx, domain.OperationFilter{
				UserID: userA,
				From:   ptr(at(t, tt.from)),
				To:     ptr(at(t, tt.to)),
			})
			require.NoError(t, err)
			assert.Len(t, got, tt.wantCount)
		})
	}
}

func TestOperationRepositoryListOrdering(t *testing.T) {
	truncate(t)
	seedOperations(t)
	ctx := testCtx(t)
	repo := postgres.NewOperationRepository(testPool)

	got, err := repo.List(ctx, domain.OperationFilter{UserID: userA})
	require.NoError(t, err)
	require.NotEmpty(t, got)

	// Порядок - от свежих к старым.
	for i := 1; i < len(got); i++ {
		assert.False(t, got[i-1].OccurredAt.Before(got[i].OccurredAt),
			"операции должны идти по убыванию occurred_at")
	}
	assert.Equal(t, at(t, "2026-08-01T00:00:00Z"), got[0].OccurredAt)
}

// Порядок стабилен при совпадающих occurred_at: вторичный ключ сортировки - id.
func TestOperationRepositoryListOrderingIsStable(t *testing.T) {
	truncate(t)
	ctx := testCtx(t)

	categories := postgres.NewCategoryRepository(testPool)
	operations := postgres.NewOperationRepository(testPool)

	category, err := categories.Create(ctx, domain.NewCategory{UserID: userA, Name: "Продукты"})
	require.NoError(t, err)

	sameMoment := at(t, "2026-07-15T12:00:00Z")
	for range 5 {
		_, err := operations.Create(ctx, domain.NewOperation{
			UserID: userA, CategoryID: category.ID, Type: domain.OperationExpense,
			Amount: 100, OccurredAt: &sameMoment,
		})
		require.NoError(t, err)
	}

	got, err := operations.List(ctx, domain.OperationFilter{UserID: userA})
	require.NoError(t, err)
	require.Len(t, got, 5)

	for i := 1; i < len(got); i++ {
		assert.Greater(t, got[i-1].ID, got[i].ID, "при равном времени сортировка идёт по убыванию id")
	}
}

// ------------------------------------------------------------ статистика ----

// Главный тест агрегации: цифры сверяются с примером ответа из ТЗ.
func TestStatsRepositoryAggregation(t *testing.T) {
	truncate(t)
	ids := seedOperations(t)
	ctx := testCtx(t)
	repo := postgres.NewStatsRepository(testPool)

	stats, err := repo.Get(ctx, userA, domain.Period{
		From: at(t, "2026-07-01T00:00:00Z"),
		To:   at(t, "2026-07-31T23:59:59.999999Z"),
	})
	require.NoError(t, err)

	assert.Equal(t, int64(20000000), stats.TotalIncome)
	assert.Equal(t, int64(6500000), stats.TotalExpense)
	assert.Equal(t, int64(13500000), stats.Balance)
	assert.Equal(t, stats.TotalIncome-stats.TotalExpense, stats.Balance,
		"разницу считает PostgreSQL, и она обязана сходиться с суммами")

	// Группировка по категориям - по убыванию суммы. Расходы «Продукты»
	// сложились из двух операций: 1 800 000 + 1 400 000.
	require.Len(t, stats.ExpensesByCategory, 3)
	assert.Equal(t, domain.CategoryExpense{
		CategoryID: ids["Продукты"], Category: "Продукты", Amount: 3200000,
	}, stats.ExpensesByCategory[0])
	assert.Equal(t, domain.CategoryExpense{
		CategoryID: ids["Аренда"], Category: "Аренда", Amount: 2500000,
	}, stats.ExpensesByCategory[1])
	assert.Equal(t, domain.CategoryExpense{
		CategoryID: ids["Подписки"], Category: "Подписки", Amount: 800000,
	}, stats.ExpensesByCategory[2])
}

// Доходы не должны попадать в разбивку по категориям: ТЗ требует группировку
// именно расходов.
func TestStatsRepositoryExcludesIncomeFromCategoryBreakdown(t *testing.T) {
	truncate(t)
	seedOperations(t)
	ctx := testCtx(t)
	repo := postgres.NewStatsRepository(testPool)

	stats, err := repo.Get(ctx, userA, domain.Period{
		From: at(t, "2026-07-01T00:00:00Z"),
		To:   at(t, "2026-07-31T23:59:59.999999Z"),
	})
	require.NoError(t, err)

	for _, item := range stats.ExpensesByCategory {
		assert.NotEqual(t, "Зарплата", item.Category, "категория дохода не должна попадать в расходы")
	}

	var sum int64
	for _, item := range stats.ExpensesByCategory {
		sum += item.Amount
	}
	assert.Equal(t, stats.TotalExpense, sum, "сумма по категориям обязана сходиться с общими расходами")
}

// Пустой период: SUM() без строк возвращает NULL, а json_agg - NULL.
// В ответе должны быть нули и пустой массив.
func TestStatsRepositoryEmptyPeriod(t *testing.T) {
	truncate(t)
	seedOperations(t)
	ctx := testCtx(t)
	repo := postgres.NewStatsRepository(testPool)

	stats, err := repo.Get(ctx, userA, domain.Period{
		From: at(t, "2020-01-01T00:00:00Z"),
		To:   at(t, "2020-12-31T23:59:59Z"),
	})
	require.NoError(t, err)

	assert.Zero(t, stats.TotalIncome)
	assert.Zero(t, stats.TotalExpense)
	assert.Zero(t, stats.Balance)
	assert.NotNil(t, stats.ExpensesByCategory, "должен быть пустой срез, а не nil")
	assert.Empty(t, stats.ExpensesByCategory)
}

func TestStatsRepositoryUnknownUser(t *testing.T) {
	truncate(t)
	seedOperations(t)
	ctx := testCtx(t)
	repo := postgres.NewStatsRepository(testPool)

	stats, err := repo.Get(ctx, uuid.MustParse("33333333-3333-4333-8333-333333333333"), domain.Period{
		From: at(t, "2026-07-01T00:00:00Z"),
		To:   at(t, "2026-07-31T23:59:59.999999Z"),
	})
	require.NoError(t, err, "неизвестный пользователь - не ошибка, а пустая статистика")
	assert.Zero(t, stats.TotalIncome)
	assert.Empty(t, stats.ExpensesByCategory)
}

// Статистика изолирована по пользователю: операции userB не должны влиять
// на цифры userA.
func TestStatsRepositoryIsolatedByUser(t *testing.T) {
	truncate(t)
	seedOperations(t)
	ctx := testCtx(t)
	repo := postgres.NewStatsRepository(testPool)

	period := domain.Period{
		From: at(t, "2026-07-01T00:00:00Z"),
		To:   at(t, "2026-07-31T23:59:59.999999Z"),
	}

	statsB, err := repo.Get(ctx, userB, period)
	require.NoError(t, err)
	assert.Equal(t, int64(777777), statsB.TotalExpense)
	assert.Equal(t, int64(-777777), statsB.Balance, "баланс может быть отрицательным")
	require.Len(t, statsB.ExpensesByCategory, 1)
	assert.Equal(t, "Прочее", statsB.ExpensesByCategory[0].Category)
}

// Суммы порядка триллионов должны считаться точно: SUM() над bigint
// возвращает numeric, и без явного приведения к bigint запрос бы падал.
func TestStatsRepositoryLargeAmounts(t *testing.T) {
	truncate(t)
	ctx := testCtx(t)

	categories := postgres.NewCategoryRepository(testPool)
	operations := postgres.NewOperationRepository(testPool)

	category, err := categories.Create(ctx, domain.NewCategory{UserID: userA, Name: "Крупное"})
	require.NoError(t, err)

	const amount = domain.MaxAmount
	const count = 5

	for i := range count {
		_, err := operations.Create(ctx, domain.NewOperation{
			UserID: userA, CategoryID: category.ID, Type: domain.OperationExpense,
			Amount: amount, OccurredAt: ptr(at(t, "2026-07-15T12:00:00Z").Add(time.Duration(i) * time.Second)),
		})
		require.NoError(t, err)
	}

	stats, err := postgres.NewStatsRepository(testPool).Get(ctx, userA, domain.Period{
		From: at(t, "2026-07-01T00:00:00Z"),
		To:   at(t, "2026-07-31T23:59:59.999999Z"),
	})
	require.NoError(t, err)

	assert.Equal(t, amount*count, stats.TotalExpense)
	assert.Equal(t, -amount*count, stats.Balance)
}
