package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"finance-api/internal/domain"
)

// OperationService - операции над финансовыми операциями.
type OperationService struct {
	repo OperationRepository
	// now вынесен в поле, чтобы правила, зависящие от текущего времени
	// (подстановка occurred_at, проверка даты в будущем), были детерминированно
	// проверяемы в тестах.
	now func() time.Time
}

// Option настраивает сервис операций.
type Option func(*OperationService)

// WithClock подменяет источник текущего времени.
//
// Единственное назначение - детерминированные тесты правил, завязанных
// на «сейчас»: подстановки occurred_at и границы допустимой даты в будущем.
func WithClock(now func() time.Time) Option {
	return func(s *OperationService) { s.now = now }
}

// NewOperationService создаёт сервис операций.
func NewOperationService(repo OperationRepository, opts ...Option) *OperationService {
	s := &OperationService{repo: repo, now: time.Now}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Create сохраняет финансовую операцию.
//
// Если occurred_at не задан, подставляется текущее время: фиксация операции
// «прямо сейчас» - самый частый сценарий, и требовать в этом случае явную
// метку времени излишне.
//
// Возвращает domain.ValidationErrors при некорректных данных
// и domain.ErrCategoryNotFound, если категория не существует или принадлежит
// другому пользователю.
func (s *OperationService) Create(ctx context.Context, in domain.NewOperation) (domain.Operation, error) {
	now := s.now().UTC()

	if in.OccurredAt == nil {
		in.OccurredAt = &now
	}
	in.Comment = normalizeComment(in.Comment)

	if err := in.Validate(now); err != nil {
		return domain.Operation{}, err
	}

	op, err := s.repo.Create(ctx, in)
	if err != nil {
		return domain.Operation{}, fmt.Errorf("create operation: %w", err)
	}
	return op, nil
}

// List возвращает операции пользователя по фильтру.
//
// Согласованность границ периода (from <= to) проверяется в транспортном слое
// при разборе query-параметров: там ошибка относится к запросу целиком,
// а не к бизнес-правилу.
func (s *OperationService) List(ctx context.Context, filter domain.OperationFilter) ([]domain.Operation, error) {
	operations, err := s.repo.List(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("list operations: %w", err)
	}
	return operations, nil
}

// normalizeComment убирает окружающие пробелы и приравнивает пустую строку
// к отсутствию комментария: в БД не должно оказаться строк из одних пробелов.
func normalizeComment(comment *string) *string {
	if comment == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*comment)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
