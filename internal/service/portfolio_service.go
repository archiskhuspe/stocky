package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/sirupsen/logrus"
	"stocky/internal/repository"
)

type PortfolioService struct {
	rewardRepo *repository.RewardRepository
	priceRepo  *repository.StockPriceRepository
}

func NewPortfolioService(
	rewardRepo *repository.RewardRepository,
	priceRepo *repository.StockPriceRepository,
) *PortfolioService {
	return &PortfolioService{
		rewardRepo: rewardRepo,
		priceRepo:  priceRepo,
	}
}

type TodayReward struct {
	StockSymbol string          `json:"stock_symbol"`
	Quantity    decimal.Decimal `json:"quantity"`
	Timestamp   time.Time       `json:"timestamp"`
	EventID     uuid.UUID       `json:"event_id"`
}

func (s *PortfolioService) GetTodayRewards(ctx context.Context, userID uuid.UUID) ([]TodayReward, error) {
	istLocation, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		return nil, fmt.Errorf("failed to load IST timezone: %w", err)
	}

	now := time.Now().In(istLocation)
	rewards, err := s.rewardRepo.GetTodayRewards(ctx, userID, now)
	if err != nil {
		return nil, err
	}

	result := make([]TodayReward, len(rewards))
	for i, r := range rewards {
		result[i] = TodayReward{
			StockSymbol: r.StockSymbol,
			Quantity:    r.Quantity,
			Timestamp:   r.Timestamp,
			EventID:     r.EventID,
		}
	}

	return result, nil
}

type HistoricalINRValue struct {
	Date     string          `json:"date"`
	INRValue decimal.Decimal `json:"inr_value"`
}

func (s *PortfolioService) GetHistoricalINR(ctx context.Context, userID uuid.UUID) ([]HistoricalINRValue, error) {
	istLocation, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		return nil, fmt.Errorf("failed to load IST timezone: %w", err)
	}

	now := time.Now().In(istLocation)
	yesterday := now.AddDate(0, 0, -1)

	var dates []time.Time
	currentDate := yesterday
	for i := 0; i < 30; i++ {
		dates = append(dates, currentDate)
		currentDate = currentDate.AddDate(0, 0, -1)
	}

	var results []HistoricalINRValue
	for _, date := range dates {
		endOfDay := time.Date(date.Year(), date.Month(), date.Day(), 23, 59, 59, 0, istLocation)

		sharesByStock, err := s.rewardRepo.GetTotalSharesByStockUpToDate(ctx, userID, endOfDay)
		if err != nil {
			logrus.WithError(err).WithField("date", date.Format("2006-01-02")).Warn("Failed to fetch shares for date; skipping")
			continue
		}

		if len(sharesByStock) == 0 {
			continue
		}

		totalValue := decimal.Zero
		for stock, qty := range sharesByStock {
			price, err := s.priceRepo.GetHistoricalPrices(ctx, stock, endOfDay)
			if err != nil {
				latestPrice, err := s.priceRepo.GetLatest(ctx, stock)
				if err != nil {
					logrus.WithError(err).WithFields(logrus.Fields{"stock": stock, "date": date.Format("2006-01-02")}).Warn("No price available for stock; skipping")
					continue
				}
				totalValue = totalValue.Add(latestPrice.Price.Mul(qty))
			} else {
				totalValue = totalValue.Add(price.Price.Mul(qty))
			}
		}

		if totalValue.GreaterThan(decimal.Zero) {
			results = append(results, HistoricalINRValue{
				Date:     date.Format("2006-01-02"),
				INRValue: totalValue.Round(2),
			})
		}
	}

	return results, nil
}

type StatsResponse struct {
	TodaySharesByStock  map[string]decimal.Decimal `json:"today_shares_by_stock"`
	CurrentPortfolioValue decimal.Decimal          `json:"current_portfolio_value"`
}

func (s *PortfolioService) GetStats(ctx context.Context, userID uuid.UUID) (*StatsResponse, error) {
	istLocation, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		return nil, fmt.Errorf("failed to load IST timezone: %w", err)
	}

	now := time.Now().In(istLocation)

	todayShares, err := s.rewardRepo.GetTotalSharesByStockToday(ctx, userID, now)
	if err != nil {
		return nil, err
	}

	allPrices, err := s.priceRepo.GetAllLatest(ctx)
	if err != nil {
		return nil, err
	}

	allShares, err := s.rewardRepo.GetTotalSharesByStockUpToDate(ctx, userID, now)
	if err != nil {
		return nil, err
	}

	portfolioValue := decimal.Zero
	for stock, qty := range allShares {
		if price, ok := allPrices[stock]; ok {
			portfolioValue = portfolioValue.Add(price.Mul(qty))
		}
	}

	return &StatsResponse{
		TodaySharesByStock:   todayShares,
		CurrentPortfolioValue: portfolioValue.Round(2),
	}, nil
}

type PortfolioHolding struct {
	StockSymbol   string          `json:"stock_symbol"`
	TotalQuantity decimal.Decimal `json:"total_quantity"`
	CurrentPrice  decimal.Decimal `json:"current_price"`
	CurrentValue  decimal.Decimal `json:"current_value"`
}

func (s *PortfolioService) GetPortfolio(ctx context.Context, userID uuid.UUID) ([]PortfolioHolding, error) {
	istLocation, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		return nil, fmt.Errorf("failed to load IST timezone: %w", err)
	}

	now := time.Now().In(istLocation)

	allShares, err := s.rewardRepo.GetTotalSharesByStockUpToDate(ctx, userID, now)
	if err != nil {
		return nil, err
	}

	allPrices, err := s.priceRepo.GetAllLatest(ctx)
	if err != nil {
		return nil, err
	}

	var holdings []PortfolioHolding
	for stock, qty := range allShares {
		price, ok := allPrices[stock]
		if !ok {
			continue
		}

		holdings = append(holdings, PortfolioHolding{
			StockSymbol:   stock,
			TotalQuantity: qty,
			CurrentPrice:  price,
			CurrentValue:  price.Mul(qty).Round(2),
		})
	}

	return holdings, nil
}

