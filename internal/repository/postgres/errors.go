package postgres

import (
	"errors"

	"finance-api/internal/domain"

	"github.com/jackc/pgx/v5/pgconn"
)

// Коды ошибок PostgreSQL (класс 23 - integrity constraint violation).
const (
	codeUniqueViolation     = "23505"
	codeForeignKeyViolation = "23503"
)

// Имена ограничений из миграции 00001_init.sql. Разбор идёт по имени, а не по
// тексту сообщения: имя стабильно и не зависит от локали сервера БД.
const (
	constraintCategoryNameUniq = "categories_user_name_uniq"
	constraintOperationCatFK   = "operations_category_fk"
)

// mapError переводит ошибку PostgreSQL в доменную.
//
// Здесь проходит граница слоёв: выше по стеку никто не знает про SQLSTATE,
// ниже - никто не знает про HTTP-статусы.
func mapError(err error) error {
	if err == nil {
		return nil
	}

	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}

	switch pgErr.Code {
	case codeUniqueViolation:
		if pgErr.ConstraintName == constraintCategoryNameUniq {
			return domain.ErrCategoryAlreadyExists
		}
	case codeForeignKeyViolation:
		// Нарушение составного FK (category_id, user_id) означает одно из двух:
		// категории не существует либо она принадлежит другому пользователю.
		// Снаружи эти случаи неразличимы намеренно - см. domain.ErrCategoryNotFound.
		if pgErr.ConstraintName == constraintOperationCatFK {
			return domain.ErrCategoryNotFound
		}
	}
	return err
}
