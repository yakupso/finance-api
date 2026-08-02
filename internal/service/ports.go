// Package service содержит бизнес-логику приложения.
//
// Интерфейсы репозиториев объявлены здесь, на стороне потребителя, а не рядом
// с их реализацией в repository/postgres. Благодаря этому сервисы не зависят
// от конкретного хранилища, а в unit-тестах подменяются простыми фейками.
package service

import (
	"context"

	"finance-api/internal/domain"
)

// CategoryRepository - хранилище категорий.
type CategoryRepository interface {
	// Create сохраняет категорию. Возвращает domain.ErrCategoryAlreadyExists,
	// если у пользователя уже есть категория с таким именем.
	Create(ctx context.Context, in domain.NewCategory) (domain.Category, error)
	// List возвращает все категории пользователя.
	List(ctx context.Context, userID domain.UserID) ([]domain.Category, error)
}

// OperationRepository - хранилище финансовых операций.
type OperationRepository interface {
	// Create сохраняет операцию. Возвращает domain.ErrCategoryNotFound, если
	// категория не существует или принадлежит другому пользователю.
	Create(ctx context.Context, in domain.NewOperation) (domain.Operation, error)
	// List возвращает операции пользователя по заданному фильтру.
	List(ctx context.Context, filter domain.OperationFilter) ([]domain.Operation, error)
}

// StatsRepository - агрегирующие запросы.
type StatsRepository interface {
	// Get возвращает статистику пользователя за период с включающими границами.
	Get(ctx context.Context, userID domain.UserID, period domain.Period) (domain.Stats, error)
}
