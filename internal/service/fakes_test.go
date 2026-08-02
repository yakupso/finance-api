package service_test

import (
	"context"
	"testing"
	"time"

	"finance-api/internal/domain"

	"github.com/google/uuid"
)

// Фейки написаны вручную, а не сгенерированы: интерфейсы репозиториев состоят
// из двух методов, и кодогенерация с её отдельным шагом сборки здесь
// не окупается. Заодно фейк хранит полученный аргумент - это позволяет
// проверять, что сервис передал в хранилище именно то, что ожидается.

type fakeCategoryRepo struct {
	createFn  func(ctx context.Context, in domain.NewCategory) (domain.Category, error)
	listFn    func(ctx context.Context, userID domain.UserID) ([]domain.Category, error)
	createGot domain.NewCategory
	createCnt int
}

func (f *fakeCategoryRepo) Create(ctx context.Context, in domain.NewCategory) (domain.Category, error) {
	f.createGot = in
	f.createCnt++
	if f.createFn != nil {
		return f.createFn(ctx, in)
	}
	return domain.Category{ID: 1, UserID: in.UserID, Name: in.Name, CreatedAt: fixedNow}, nil
}

func (f *fakeCategoryRepo) List(ctx context.Context, userID domain.UserID) ([]domain.Category, error) {
	if f.listFn != nil {
		return f.listFn(ctx, userID)
	}
	return nil, nil
}

type fakeOperationRepo struct {
	createFn  func(ctx context.Context, in domain.NewOperation) (domain.Operation, error)
	listFn    func(ctx context.Context, filter domain.OperationFilter) ([]domain.Operation, error)
	createGot domain.NewOperation
	listGot   domain.OperationFilter
	createCnt int
}

func (f *fakeOperationRepo) Create(ctx context.Context, in domain.NewOperation) (domain.Operation, error) {
	f.createGot = in
	f.createCnt++
	if f.createFn != nil {
		return f.createFn(ctx, in)
	}
	occurredAt := time.Time{}
	if in.OccurredAt != nil {
		occurredAt = *in.OccurredAt
	}
	return domain.Operation{
		ID:         1,
		UserID:     in.UserID,
		Category:   domain.CategoryRef{ID: in.CategoryID, Name: "Продукты"},
		Type:       in.Type,
		Amount:     in.Amount,
		Comment:    in.Comment,
		OccurredAt: occurredAt,
		CreatedAt:  fixedNow,
	}, nil
}

func (f *fakeOperationRepo) List(ctx context.Context, filter domain.OperationFilter) ([]domain.Operation, error) {
	f.listGot = filter
	if f.listFn != nil {
		return f.listFn(ctx, filter)
	}
	return nil, nil
}

type fakeStatsRepo struct {
	getFn     func(ctx context.Context, userID domain.UserID, period domain.Period) (domain.Stats, error)
	gotUserID domain.UserID
	gotPeriod domain.Period
}

func (f *fakeStatsRepo) Get(ctx context.Context, userID domain.UserID, period domain.Period) (domain.Stats, error) {
	f.gotUserID = userID
	f.gotPeriod = period
	if f.getFn != nil {
		return f.getFn(ctx, userID, period)
	}
	return domain.Stats{Period: period}, nil
}

var (
	testUser = uuid.MustParse("11111111-1111-4111-8111-111111111111")
	fixedNow = time.Date(2026, time.July, 20, 12, 0, 0, 0, time.UTC)
)

func ptr[T any](v T) *T { return &v }

// ctx возвращает контекст, автоматически отменяемый по завершении теста.
func ctx(t *testing.T) context.Context {
	t.Helper()
	c, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return c
}
