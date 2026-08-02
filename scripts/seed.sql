-- Демонстрационные данные для примеров из README.
--
-- Скрипт идемпотентен: повторный запуск не создаёт дублей и не меняет цифры.
-- Данные привязаны к фиксированному пользователю, чтобы примеры с curl
-- работали копированием как есть.
--
-- Применение:
--     make seed
--     psql "$DATABASE_URL" -f scripts/seed.sql
--
-- Через docker compose:
--     docker compose exec -T postgres psql -U finance -d finance < scripts/seed.sql

BEGIN;

-- Демо-пользователь из README.
\set demo_user '''11111111-1111-4111-8111-111111111111'''

-- Удаляем прежние демо-данные, чтобы повторный запуск давал тот же результат.
-- Порядок важен: на categories ссылается FK с ON DELETE RESTRICT.
DELETE FROM operations WHERE user_id = :demo_user::uuid;
DELETE FROM categories WHERE user_id = :demo_user::uuid;

INSERT INTO categories (user_id, name) VALUES
    (:demo_user::uuid, 'Зарплата'),
    (:demo_user::uuid, 'Продукты'),
    (:demo_user::uuid, 'Аренда'),
    (:demo_user::uuid, 'Подписки');

-- Суммы в копейках. Набор подобран так, чтобы статистика за июль 2026
-- совпадала с примером ответа из технического задания:
--   доходы 20 000 000, расходы 6 500 000, разница 13 500 000.
INSERT INTO operations (user_id, category_id, type, amount, comment, occurred_at)
SELECT :demo_user::uuid, c.id, v.type::operation_type, v.amount, v.comment, v.occurred_at
FROM (VALUES
    ('Зарплата', 'income',  20000000::bigint, 'Аванс и основная часть', TIMESTAMPTZ '2026-07-01 09:00:00+00'),
    ('Продукты', 'expense',  1800000::bigint, 'Еженедельная закупка',   TIMESTAMPTZ '2026-07-05 18:20:00+00'),
    ('Аренда',   'expense',  2500000::bigint, 'Аренда квартиры',        TIMESTAMPTZ '2026-07-10 12:00:00+00'),
    ('Продукты', 'expense',  1400000::bigint, NULL,                     TIMESTAMPTZ '2026-07-15 18:32:00+00'),
    ('Подписки', 'expense',   800000::bigint, 'Музыка и кино',          TIMESTAMPTZ '2026-07-20 08:15:00+00'),
    -- Операция за пределами июля: показывает, что фильтр по периоду работает.
    ('Продукты', 'expense',   950000::bigint, 'Уже август',             TIMESTAMPTZ '2026-08-02 11:00:00+00')
) AS v(category_name, type, amount, comment, occurred_at)
JOIN categories c
  ON c.user_id = :demo_user::uuid AND c.name = v.category_name;

COMMIT;

\echo 'Демо-данные загружены. Пользователь: 11111111-1111-4111-8111-111111111111'
