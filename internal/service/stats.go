package service

import (
	"context"
	"fmt"

	"finance-api/internal/domain"
)

// StatsService - агрегированная статистика по операциям пользователя.
type StatsService struct {
	repo StatsRepository
}

// NewStatsService создаёт сервис статистики.
func NewStatsService(repo StatsRepository) *StatsService {
	return &StatsService{repo: repo}
}

// Get возвращает статистику пользователя за период с включающими границами.
//
// Вся агрегация выполняется на стороне PostgreSQL: сервис не производит
// никаких вычислений над суммами и только передаёт результат дальше.
func (s *StatsService) Get(ctx context.Context, userID domain.UserID, period domain.Period) (domain.Stats, error) {
	stats, err := s.repo.Get(ctx, userID, period)
	if err != nil {
		return domain.Stats{}, fmt.Errorf("get stats: %w", err)
	}
	return stats, nil
}
