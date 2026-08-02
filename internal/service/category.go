package service

import (
	"context"
	"fmt"

	"finance-api/internal/domain"
)

// CategoryService - операции над категориями.
type CategoryService struct {
	repo CategoryRepository
}

// NewCategoryService создаёт сервис категорий.
func NewCategoryService(repo CategoryRepository) *CategoryService {
	return &CategoryService{repo: repo}
}

// Create нормализует название и сохраняет категорию.
//
// Возвращает domain.ValidationErrors при некорректных данных
// и domain.ErrCategoryAlreadyExists, если имя уже занято.
func (s *CategoryService) Create(ctx context.Context, in domain.NewCategory) (domain.Category, error) {
	in.Name = domain.NormalizeCategoryName(in.Name)

	if err := in.Validate(); err != nil {
		return domain.Category{}, err
	}

	category, err := s.repo.Create(ctx, in)
	if err != nil {
		return domain.Category{}, fmt.Errorf("create category: %w", err)
	}
	return category, nil
}

// List возвращает категории пользователя, отсортированные по названию.
func (s *CategoryService) List(ctx context.Context, userID domain.UserID) ([]domain.Category, error) {
	categories, err := s.repo.List(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list categories: %w", err)
	}
	return categories, nil
}
