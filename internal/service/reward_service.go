package service

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/shopspring/decimal"
	"github.com/sirupsen/logrus"
	"stocky/internal/models"
	"stocky/internal/repository"
	"stocky/pkg/fees"
)

type RewardService struct {
	rewardRepo *repository.RewardRepository
	ledgerRepo *repository.LedgerRepository
	userRepo   *repository.UserRepository
	priceRepo  *repository.StockPriceRepository
	db         *sqlx.DB
}

func NewRewardService(
	rewardRepo *repository.RewardRepository,
	ledgerRepo *repository.LedgerRepository,
	userRepo *repository.UserRepository,
	priceRepo *repository.StockPriceRepository,
	db *sqlx.DB,
) *RewardService {
	return &RewardService{
		rewardRepo: rewardRepo,
		ledgerRepo: ledgerRepo,
		userRepo:   userRepo,
		priceRepo:  priceRepo,
		db:         db,
	}
}

type RewardRequest struct {
	UserID      uuid.UUID       `json:"user_id" binding:"required"`
	StockSymbol string          `json:"stock_symbol" binding:"required"`
	Quantity    decimal.Decimal `json:"quantity" binding:"required"`
	Timestamp   time.Time       `json:"timestamp" binding:"required"`
	EventID     uuid.UUID       `json:"event_id" binding:"required"`
}

func (s *RewardService) ProcessReward(ctx context.Context, req RewardRequest) error {
	existing, err := s.rewardRepo.GetByEventID(ctx, req.EventID)
	if err == nil && existing != nil {
		logrus.WithField("event_id", req.EventID).Info("Reward event already processed (idempotent)")
		return nil
	}
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("failed to check idempotency: %w", err)
	}

	_, err = s.userRepo.GetOrCreate(ctx, req.UserID)
	if err != nil {
		return fmt.Errorf("failed to get/create user: %w", err)
	}

	stockPrice, err := s.priceRepo.GetLatest(ctx, req.StockSymbol)
	if err != nil {
		return fmt.Errorf("failed to get stock price for %s: %w", req.StockSymbol, err)
	}

	totalFees := fees.CalculateFees(stockPrice.Price, req.Quantity)
	transactionValue := stockPrice.Price.Mul(req.Quantity)
	totalCost := transactionValue.Add(totalFees)

	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	reward := &models.RewardEvent{
		ID:          uuid.New(),
		EventID:     req.EventID,
		UserID:      req.UserID,
		StockSymbol: req.StockSymbol,
		Quantity:    req.Quantity,
		Timestamp:   req.Timestamp,
		CreatedAt:   time.Now(),
	}

	if err := s.rewardRepo.Create(ctx, tx, reward); err != nil {
		return fmt.Errorf("failed to create reward: %w", err)
	}

	entries := []*models.LedgerEntry{
		{
			ID:        uuid.New(),
			EventID:   req.EventID,
			EntryType: models.LedgerEntryTypeStock,
			Symbol:    &req.StockSymbol,
			Debit:     decimal.Zero,
			Credit:    transactionValue,
			CreatedAt: time.Now(),
		},
		{
			ID:        uuid.New(),
			EventID:   req.EventID,
			EntryType: models.LedgerEntryTypeCash,
			Symbol:    nil,
			Debit:     totalCost,
			Credit:    decimal.Zero,
			CreatedAt: time.Now(),
		},
		{
			ID:        uuid.New(),
			EventID:   req.EventID,
			EntryType: models.LedgerEntryTypeFee,
			Symbol:    nil,
			Debit:     decimal.Zero,
			Credit:    totalFees,
			CreatedAt: time.Now(),
		},
	}

	for _, entry := range entries {
		if err := s.ledgerRepo.Create(ctx, tx, entry); err != nil {
			return fmt.Errorf("failed to create ledger entry: %w", err)
		}
	}

	totalDebit := decimal.Zero
	totalCredit := decimal.Zero
	for _, entry := range entries {
		totalDebit = totalDebit.Add(entry.Debit)
		totalCredit = totalCredit.Add(entry.Credit)
	}

	if !totalDebit.Equal(totalCredit) {
		return fmt.Errorf("ledger imbalance: debit=%s, credit=%s", totalDebit, totalCredit)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	logrus.WithFields(logrus.Fields{
		"event_id":     req.EventID,
		"user_id":      req.UserID,
		"stock_symbol": req.StockSymbol,
		"quantity":     req.Quantity,
		"total_cost":   totalCost,
		"fees":         totalFees,
	}).Info("Reward processed successfully")

	return nil
}

