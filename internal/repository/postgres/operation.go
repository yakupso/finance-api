package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"finance-api/internal/domain"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// OperationRepository - доступ к таблице operations.
type OperationRepository struct {
	pool *pgxpool.Pool
}

// NewOperationRepository создаёт репозиторий операций.
func NewOperationRepository(pool *pgxpool.Pool) *OperationRepository {
	return &OperationRepository{pool: pool}
}

// INSERT завёрнут в CTE, чтобы одним запросом вернуть операцию вместе с именем
// категории: RETURNING сам по себе не умеет джойнить.
//
// $3::operation_type - явное приведение: параметр уходит как текст, а PostgreSQL
// приводит его к пользовательскому ENUM. Без каста pgx пришлось бы сообщать
// про OID пользовательского типа, что означало бы регистрацию кодека на каждом
// соединении пула.
const createOperationQuery = `
WITH inserted AS (
    INSERT INTO operations (user_id, category_id, type, amount, comment, occurred_at)
    VALUES ($1, $2, $3::operation_type, $4, $5, $6)
    RETURNING id, user_id, category_id, type, amount, comment, occurred_at, created_at
)
SELECT i.id, i.user_id, c.id, c.name, i.type, i.amount, i.comment, i.occurred_at, i.created_at
FROM inserted i
JOIN categories c ON c.id = i.category_id AND c.user_id = i.user_id`

// Create сохраняет операцию.
//
// Принадлежность категории тому же пользователю проверяет составной внешний
// ключ operations_category_fk, поэтому предварительный SELECT не нужен -
// и невозможна гонка между проверкой и вставкой. Нарушение FK превращается
// в domain.ErrCategoryNotFound.
func (r *OperationRepository) Create(ctx context.Context, in domain.NewOperation) (domain.Operation, error) {
	// К этому моменту OccurredAt заполнен сервисом; проверка страхует от
	// разыменования nil при вызове репозитория в обход сервиса - вставить
	// нулевую дату не даст CHECK-ограничение operations_occurred_at_sane.
	var occurredAt time.Time
	if in.OccurredAt != nil {
		occurredAt = *in.OccurredAt
	}

	rows, err := r.pool.Query(ctx, createOperationQuery,
		in.UserID,
		in.CategoryID,
		string(in.Type),
		in.Amount,
		in.Comment,
		occurredAt,
	)
	if err != nil {
		return domain.Operation{}, fmt.Errorf("insert operation: %w", mapError(err))
	}

	op, err := pgx.CollectExactlyOneRow(rows, scanOperation)
	if err != nil {
		return domain.Operation{}, fmt.Errorf("insert operation: %w", mapError(err))
	}
	return op, nil
}

const selectOperationsPrefix = `
SELECT o.id, o.user_id, c.id, c.name, o.type, o.amount, o.comment, o.occurred_at, o.created_at
FROM operations o
JOIN categories c ON c.id = o.category_id AND c.user_id = o.user_id
WHERE `

// Порядок стабилен: при совпадающем occurred_at операции упорядочиваются по id.
const selectOperationsSuffix = `
ORDER BY o.occurred_at DESC, o.id DESC`

// List возвращает операции пользователя, отфильтрованные по периоду, типу
// и категории. Пагинация не реализуется - по условиям задания она не требуется.
func (r *OperationRepository) List(ctx context.Context, filter domain.OperationFilter) ([]domain.Operation, error) {
	query, args := buildListQuery(filter)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("select operations: %w", mapError(err))
	}

	operations, err := pgx.CollectRows(rows, scanOperation)
	if err != nil {
		return nil, fmt.Errorf("select operations: %w", mapError(err))
	}
	return operations, nil
}

// buildListQuery собирает WHERE из опциональных фильтров.
//
// В строку запроса подставляются только номера плейсхолдеров ($1, $2, ...),
// сами значения всегда уходят отдельными аргументами: пользовательский ввод
// в текст SQL не попадает ни при каком наборе фильтров.
func buildListQuery(filter domain.OperationFilter) (string, []any) {
	args := make([]any, 0, 5)
	conditions := make([]string, 0, 5)

	placeholder := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}

	conditions = append(conditions, "o.user_id = "+placeholder(filter.UserID))

	// Обе границы периода включающие: occurred_at BETWEEN from AND to.
	// Нормализация даты без времени в конец суток выполняется в transport.
	if filter.From != nil {
		conditions = append(conditions, "o.occurred_at >= "+placeholder(*filter.From))
	}
	if filter.To != nil {
		conditions = append(conditions, "o.occurred_at <= "+placeholder(*filter.To))
	}
	if filter.Type != nil {
		conditions = append(conditions, "o.type = "+placeholder(string(*filter.Type))+"::operation_type")
	}
	if filter.CategoryID != nil {
		conditions = append(conditions, "o.category_id = "+placeholder(*filter.CategoryID))
	}

	var sb strings.Builder
	sb.WriteString(selectOperationsPrefix)
	sb.WriteString(strings.Join(conditions, "\n  AND "))
	sb.WriteString(selectOperationsSuffix)

	return sb.String(), args
}

func scanOperation(row pgx.CollectableRow) (domain.Operation, error) {
	var (
		op      domain.Operation
		opType  string
		comment *string
	)
	err := row.Scan(
		&op.ID,
		&op.UserID,
		&op.Category.ID,
		&op.Category.Name,
		&opType,
		&op.Amount,
		&comment,
		&op.OccurredAt,
		&op.CreatedAt,
	)
	if err != nil {
		return domain.Operation{}, err
	}
	op.Type = domain.OperationType(opType)
	op.Comment = comment
	// См. комментарий в scanCategory: домен всегда оперирует временем в UTC.
	op.OccurredAt = op.OccurredAt.UTC()
	op.CreatedAt = op.CreatedAt.UTC()
	return op, nil
}
