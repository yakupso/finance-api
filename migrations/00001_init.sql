-- +goose Up
-- +goose StatementBegin

CREATE TYPE operation_type AS ENUM ('income', 'expense');

-- Пользователь - внешняя сущность,
-- user_id приходит в заголовке запроса и служит признаком принадлежности данных.
CREATE TABLE categories (
    id         BIGSERIAL   PRIMARY KEY,
    user_id    UUID        NOT NULL,
    name       TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT categories_name_not_blank CHECK (btrim(name) <> ''),
    CONSTRAINT categories_name_len       CHECK (char_length(name) <= 100),

    CONSTRAINT categories_id_user_uniq   UNIQUE (id, user_id)
);

COMMENT ON CONSTRAINT categories_id_user_uniq ON categories IS
    'Требуется как цель составного FK operations_category_fk';

-- Имя категории уникально в пределах пользователя и регистронезависимо:
-- "Продукты" и "продукты" - одна и та же категория
CREATE UNIQUE INDEX categories_user_name_uniq ON categories (user_id, lower(name));

CREATE TABLE operations (
    id          BIGSERIAL      PRIMARY KEY,
    user_id     UUID           NOT NULL,
    category_id BIGINT         NOT NULL,
    type        operation_type NOT NULL,
    -- Сумма в копейках и всегда положительна: доход/расход несёт поле type
    amount      BIGINT         NOT NULL,
    comment     TEXT,
    occurred_at TIMESTAMPTZ    NOT NULL,
    created_at  TIMESTAMPTZ    NOT NULL DEFAULT now(),

    CONSTRAINT operations_amount_positive CHECK (amount > 0),
    CONSTRAINT operations_comment_len     CHECK (comment IS NULL OR char_length(comment) <= 500),
    -- Нижняя граница отсекает заведомо невалидные даты. Дублирует domain.MinOccurredAt
    CONSTRAINT operations_occurred_at_sane CHECK (occurred_at >= TIMESTAMPTZ '2000-01-01 00:00:00+00'),

    -- Ссылка идёт на пару (id, user_id), поэтому привязать операцию одного пользователя к категории другого невозможно на уровне БД
    CONSTRAINT operations_category_fk FOREIGN KEY (category_id, user_id)
        REFERENCES categories (id, user_id) ON DELETE RESTRICT
);

COMMENT ON CONSTRAINT operations_category_fk ON operations IS
    'Категория обязана принадлежать тому же пользователю, что и операция';

-- Список операций с фильтром по периоду
CREATE INDEX operations_user_occurred_idx ON operations (user_id, occurred_at DESC);
-- Список и статистика с фильтром по типу операции
CREATE INDEX operations_user_type_occurred_idx ON operations (user_id, type, occurred_at DESC);
-- Проверка ON DELETE RESTRICT со стороны ссылающейся таблицы
CREATE INDEX operations_category_idx ON operations (category_id, user_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS operations;
DROP TABLE IF EXISTS categories;
DROP TYPE IF EXISTS operation_type;

-- +goose StatementEnd
