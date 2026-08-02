package http

import (
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Семантика периода - единственное место, где задание допускает двоякое
// толкование, поэтому границы проверяются отдельно и подробно.
func TestParseTimeBound(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		upper   bool
		want    time.Time
		wantErr bool
	}{
		{
			name: "дата как нижняя граница - начало суток",
			raw:  "2026-07-01",
			want: time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:  "дата как верхняя граница - конец суток",
			raw:   "2026-07-31",
			upper: true,
			// Микросекунда - предел точности timestamptz, поэтому более
			// позднего момента внутри 31 июля в базе не существует.
			want: time.Date(2026, time.July, 31, 23, 59, 59, 999999000, time.UTC),
		},
		{
			name: "RFC 3339 используется как есть",
			raw:  "2026-07-15T18:32:00Z",
			want: time.Date(2026, time.July, 15, 18, 32, 0, 0, time.UTC),
		},
		{
			name:  "RFC 3339 как верхняя граница не расширяется до конца суток",
			raw:   "2026-07-15T18:32:00Z",
			upper: true,
			want:  time.Date(2026, time.July, 15, 18, 32, 0, 0, time.UTC),
		},
		{
			name: "смещение часового пояса сохраняется",
			raw:  "2026-07-15T18:32:00+03:00",
			want: time.Date(2026, time.July, 15, 18, 32, 0, 0, time.FixedZone("", 3*60*60)),
		},
		{name: "пустая строка", raw: "", wantErr: true},
		{name: "русское слово", raw: "вчера", wantErr: true},
		{name: "формат ДД.ММ.ГГГГ", raw: "15.07.2026", wantErr: true},
		{name: "несуществующая дата", raw: "2026-02-30", wantErr: true},
		{name: "только год", raw: "2026", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseTimeBound("from", tt.raw, tt.upper)
			if tt.wantErr {
				require.Error(t, err)
				var apiErr *apiError
				require.ErrorAs(t, err, &apiErr)
				assert.Equal(t, 400, apiErr.Status, "некорректный query-параметр - это 400")
				return
			}
			require.NoError(t, err)
			assert.True(t, tt.want.Equal(got), "ожидалось %s, получено %s", tt.want, got)
		})
	}
}

func TestParsePeriodBounds(t *testing.T) {
	t.Parallel()

	t.Run("обе границы отсутствуют", func(t *testing.T) {
		t.Parallel()
		from, to, err := parsePeriodBounds(url.Values{})
		require.NoError(t, err)
		assert.Nil(t, from)
		assert.Nil(t, to)
	})

	t.Run("только нижняя граница", func(t *testing.T) {
		t.Parallel()
		from, to, err := parsePeriodBounds(url.Values{"from": {"2026-07-01"}})
		require.NoError(t, err)
		require.NotNil(t, from)
		assert.Nil(t, to)
	})

	t.Run("from позже to отвергается", func(t *testing.T) {
		t.Parallel()
		_, _, err := parsePeriodBounds(url.Values{"from": {"2026-08-01"}, "to": {"2026-07-01"}})
		require.Error(t, err)
		var apiErr *apiError
		require.ErrorAs(t, err, &apiErr)
		assert.Equal(t, 400, apiErr.Status)
		assert.Equal(t, codeInvalidQueryParam, apiErr.Code)
	})

	t.Run("совпадающие границы допустимы", func(t *testing.T) {
		t.Parallel()
		// from=to=2026-07-15 даёт непустой интервал: нижняя граница -
		// начало суток, верхняя - их конец.
		from, to, err := parsePeriodBounds(url.Values{"from": {"2026-07-15"}, "to": {"2026-07-15"}})
		require.NoError(t, err)
		require.NotNil(t, from)
		require.NotNil(t, to)
		assert.True(t, to.After(*from), "период из одного дня не должен быть пустым")
	})
}

func TestParseRequiredPeriod(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		query   url.Values
		wantErr bool
	}{
		{
			name:  "обе границы заданы",
			query: url.Values{"from": {"2026-07-01"}, "to": {"2026-07-31"}},
		},
		{name: "обе границы отсутствуют", query: url.Values{}, wantErr: true},
		{name: "нет to", query: url.Values{"from": {"2026-07-01"}}, wantErr: true},
		{name: "нет from", query: url.Values{"to": {"2026-07-31"}}, wantErr: true},
		{name: "from пустая строка", query: url.Values{"from": {""}, "to": {"2026-07-31"}}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := parseRequiredPeriod(tt.query)
			if tt.wantErr {
				require.Error(t, err)
				var apiErr *apiError
				require.ErrorAs(t, err, &apiErr)
				assert.Equal(t, 400, apiErr.Status)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestParseOperationType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		wantNil bool
		wantErr bool
	}{
		{name: "отсутствует", raw: "", wantNil: true},
		{name: "income", raw: "income"},
		{name: "expense", raw: "expense"},
		{name: "неизвестное значение", raw: "transfer", wantErr: true},
		{name: "чувствителен к регистру", raw: "Income", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseOperationType(url.Values{"type": {tt.raw}})
			switch {
			case tt.wantErr:
				require.Error(t, err)
			case tt.wantNil:
				require.NoError(t, err)
				assert.Nil(t, got)
			default:
				require.NoError(t, err)
				require.NotNil(t, got)
				assert.Equal(t, tt.raw, got.String())
			}
		})
	}
}

func TestParseCategoryID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		want    *int64
		wantErr bool
	}{
		{name: "отсутствует", raw: ""},
		{name: "положительное число", raw: "42", want: ptrInt64(42)},
		{name: "ноль", raw: "0", wantErr: true},
		{name: "отрицательное", raw: "-1", wantErr: true},
		{name: "не число", raw: "abc", wantErr: true},
		{name: "дробное", raw: "1.5", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseCategoryID(url.Values{"category_id": {tt.raw}})
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tt.want == nil {
				assert.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			assert.Equal(t, *tt.want, *got)
		})
	}
}

func ptrInt64(v int64) *int64 { return &v }
