package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"finance-api/internal/domain"

	"github.com/jackc/pgx/v5/pgxpool"
)

// StatsRepository - агрегирующие запросы по операциям.
type StatsRepository struct {
	pool *pgxpool.Pool
}

// NewStatsRepository создаёт репозиторий статистики.
func NewStatsRepository(pool *pgxpool.Pool) *StatsRepository {
	return &StatsRepository{pool: pool}
}

// statsQuery считает всю статистику целиком на стороне PostgreSQL за один
// round-trip. В Go не выполняется ни одного арифметического действия над
// суммами - это прямое требование задания.
//
// Разбор по частям:
//
//   - filtered - единственное обращение к operations; обе последующие агрегации
//     работают уже по нему;
//   - SUM(...) FILTER (WHERE ...) считает доходы и расходы за один проход,
//     без двух отдельных подзапросов;
//   - ::bigint обязателен: SUM() над bigint в PostgreSQL возвращает numeric,
//     и Scan в int64 без приведения завершится ошибкой;
//   - COALESCE(..., 0) нужен для пустого периода: SUM() без строк даёт NULL,
//     а в ответе API должны быть нули;
//   - ORDER BY стоит внутри json_agg, а не в CTE by_category: порядок строк,
//     заданный в CTE, не гарантируется при чтении из него;
//   - COALESCE(..., '[]'::json) - json_agg без строк тоже возвращает NULL,
//     а в ответе должен быть пустой массив.
const statsQuery = `
WITH filtered AS (
    SELECT type, amount, category_id
    FROM operations
    WHERE user_id = $1
      AND occurred_at >= $2
      AND occurred_at <= $3
),
totals AS (
    SELECT
        COALESCE(SUM(amount) FILTER (WHERE type = 'income'),  0)::bigint AS total_income,
        COALESCE(SUM(amount) FILTER (WHERE type = 'expense'), 0)::bigint AS total_expense
    FROM filtered
),
by_category AS (
    SELECT c.id AS category_id, c.name AS category, SUM(f.amount)::bigint AS amount
    FROM filtered f
    JOIN categories c ON c.id = f.category_id
    WHERE f.type = 'expense'
    GROUP BY c.id, c.name
)
SELECT
    t.total_income,
    t.total_expense,
    t.total_income - t.total_expense AS balance,
    COALESCE((
        SELECT json_agg(
                   json_build_object(
                       'category_id', b.category_id,
                       'category',    b.category,
                       'amount',      b.amount
                   )
                   ORDER BY b.amount DESC, b.category
               )
        FROM by_category b
    ), '[]'::json) AS expenses_by_category
FROM totals t`

// categoryExpenseRow - промежуточное представление элемента json-агрегата.
// Существует только чтобы разобрать вывод json_agg; наружу отдаётся домен.
type categoryExpenseRow struct {
	CategoryID int64  `json:"category_id"`
	Category   string `json:"category"`
	Amount     int64  `json:"amount"`
}

// Get возвращает статистику пользователя за период.
// Обе границы периода включающие.
func (r *StatsRepository) Get(ctx context.Context, userID domain.UserID, period domain.Period) (domain.Stats, error) {
	var (
		stats   domain.Stats
		rawJSON []byte
	)

	err := r.pool.QueryRow(ctx, statsQuery, userID, period.From, period.To).Scan(
		&stats.TotalIncome,
		&stats.TotalExpense,
		&stats.Balance,
		&rawJSON,
	)
	if err != nil {
		return domain.Stats{}, fmt.Errorf("select stats: %w", mapError(err))
	}

	var rows []categoryExpenseRow
	if err := json.Unmarshal(rawJSON, &rows); err != nil {
		return domain.Stats{}, fmt.Errorf("decode expenses by category: %w", err)
	}

	stats.Period = period
	stats.ExpensesByCategory = make([]domain.CategoryExpense, 0, len(rows))
	for _, row := range rows {
		stats.ExpensesByCategory = append(stats.ExpensesByCategory, domain.CategoryExpense{
			CategoryID: row.CategoryID,
			Category:   row.Category,
			Amount:     row.Amount,
		})
	}
	return stats, nil
}
