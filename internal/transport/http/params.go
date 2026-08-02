package http

import (
	"net/url"
	"strconv"
	"strings"
	"time"

	"finance-api/internal/domain"
)

// dateLayout - формат даты без времени.
const dateLayout = "2006-01-02"

// endOfDayOffset - сдвиг от начала суток до последнего представимого момента
// внутри них. Микросекунда - предел точности timestamptz в PostgreSQL,
// поэтому более позднего значения в пределах суток в БД не существует.
const endOfDayOffset = 24*time.Hour - time.Microsecond

// parseTimeBound разбирает границу периода.
//
// Принимается два формата, и обе границы включающие:
//
//	2026-07-31              - дата без времени;
//	2026-07-31T12:00:00Z    - момент времени по RFC 3339.
//
// Для верхней границы дата без времени разворачивается в конец суток:
// to=2026-07-31 включает операции, совершённые 31 июля. Это соответствует
// интуитивному пониманию «период с 1 по 31 июля» и примеру ответа из задания.
// Дата без времени трактуется в UTC; при необходимости клиент передаёт
// явное смещение в формате RFC 3339.
func parseTimeBound(param, raw string, upper bool) (time.Time, error) {
	raw = strings.TrimSpace(raw)

	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t, nil
	}

	if d, err := time.Parse(dateLayout, raw); err == nil {
		if upper {
			return d.Add(endOfDayOffset), nil
		}
		return d, nil
	}

	return time.Time{}, badRequest(codeInvalidQueryParam,
		"%s must be a date (2006-01-02) or an RFC 3339 timestamp (2006-01-02T15:04:05Z), got %q",
		param, raw)
}

// parsePeriodBounds разбирает необязательные границы периода из query-строки.
// Возвращает nil для незаданной границы.
func parsePeriodBounds(query url.Values) (from, to *time.Time, err error) {
	if raw := query.Get("from"); raw != "" {
		v, parseErr := parseTimeBound("from", raw, false)
		if parseErr != nil {
			return nil, nil, parseErr
		}
		from = &v
	}
	if raw := query.Get("to"); raw != "" {
		v, parseErr := parseTimeBound("to", raw, true)
		if parseErr != nil {
			return nil, nil, parseErr
		}
		to = &v
	}
	if from != nil && to != nil && from.After(*to) {
		return nil, nil, badRequest(codeInvalidQueryParam,
			"from must not be later than to (from=%s, to=%s)",
			from.Format(time.RFC3339), to.Format(time.RFC3339))
	}
	return from, to, nil
}

// parseRequiredPeriod разбирает период, обе границы которого обязательны.
// Используется статистикой: задание требует агрегировать «за указанный период»,
// поэтому подставлять границы по умолчанию было бы догадкой за клиента.
func parseRequiredPeriod(query url.Values) (domain.Period, error) {
	rawFrom, rawTo := query.Get("from"), query.Get("to")

	var missing []string
	if strings.TrimSpace(rawFrom) == "" {
		missing = append(missing, "from")
	}
	if strings.TrimSpace(rawTo) == "" {
		missing = append(missing, "to")
	}
	if len(missing) > 0 {
		return domain.Period{}, badRequest(codeInvalidQueryParam,
			"query parameters %s are required", strings.Join(missing, " and "))
	}

	from, to, err := parsePeriodBounds(query)
	if err != nil {
		return domain.Period{}, err
	}
	return domain.Period{From: *from, To: *to}, nil
}

// parseOperationType разбирает необязательный фильтр по типу операции.
func parseOperationType(query url.Values) (*domain.OperationType, error) {
	raw := strings.TrimSpace(query.Get("type"))
	if raw == "" {
		return nil, nil
	}

	opType := domain.OperationType(raw)
	if !opType.IsValid() {
		return nil, badRequest(codeInvalidQueryParam,
			"type must be one of [%s], got %q", joinOperationTypes(), raw)
	}
	return &opType, nil
}

// parseCategoryID разбирает необязательный фильтр по категории.
func parseCategoryID(query url.Values) (*int64, error) {
	raw := strings.TrimSpace(query.Get("category_id"))
	if raw == "" {
		return nil, nil
	}

	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return nil, badRequest(codeInvalidQueryParam,
			"category_id must be a positive integer, got %q", raw)
	}
	return &id, nil
}

func joinOperationTypes() string {
	types := domain.OperationTypes()
	parts := make([]string, 0, len(types))
	for _, t := range types {
		parts = append(parts, t.String())
	}
	return strings.Join(parts, " ")
}
