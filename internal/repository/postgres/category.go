package postgres

import (
	"context"
	"fmt"

	"finance-api/internal/domain"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// CategoryRepository - доступ к таблице categories.
type CategoryRepository struct {
	pool *pgxpool.Pool
}

// NewCategoryRepository создаёт репозиторий категорий.
func NewCategoryRepository(pool *pgxpool.Pool) *CategoryRepository {
	return &CategoryRepository{pool: pool}
}

const createCategoryQuery = `
INSERT INTO categories (user_id, name)
VALUES ($1, $2)
RETURNING id, user_id, name, created_at`

// Create сохраняет новую категорию.
func (r *CategoryRepository) Create(ctx context.Context, in domain.NewCategory) (domain.Category, error) {
	rows, err := r.pool.Query(ctx, createCategoryQuery, in.UserID, in.Name)
	if err != nil {
		return domain.Category{}, fmt.Errorf("insert category: %w", mapError(err))
	}

	category, err := pgx.CollectExactlyOneRow(rows, scanCategory)
	if err != nil {
		return domain.Category{}, fmt.Errorf("insert category: %w", mapError(err))
	}
	return category, nil
}

const listCategoriesQuery = `
SELECT id, user_id, name, created_at
FROM categories
WHERE user_id = $1
ORDER BY name`

// List возвращает все категории пользователя, отсортированные по имени.
func (r *CategoryRepository) List(ctx context.Context, userID domain.UserID) ([]domain.Category, error) {
	rows, err := r.pool.Query(ctx, listCategoriesQuery, userID)
	if err != nil {
		return nil, fmt.Errorf("select categories: %w", mapError(err))
	}

	categories, err := pgx.CollectRows(rows, scanCategory)
	if err != nil {
		return nil, fmt.Errorf("select categories: %w", mapError(err))
	}
	return categories, nil
}

func scanCategory(row pgx.CollectableRow) (domain.Category, error) {
	var c domain.Category
	if err := row.Scan(&c.ID, &c.UserID, &c.Name, &c.CreatedAt); err != nil {
		return domain.Category{}, err
	}
	// pgx декодирует timestamptz в локальную зону процесса. Домен всегда оперирует UTC, а API отдаёт метки со суффиксом Z
	c.CreatedAt = c.CreatedAt.UTC()
	return c, nil
}
