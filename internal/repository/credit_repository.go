package repository

import (
	"errors"
	"time"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"gorm.io/gorm"
)

// ErrInsufficientCredits is returned when a debit would take a student's balance
// below zero.
var ErrInsufficientCredits = errors.New("insufficient credits")

// CreditRepository moves credits and records the ledger, all inside one
// transaction so the balance and the audit trail never diverge.
type CreditRepository struct {
	db *gorm.DB
}

// NewCreditRepository builds a CreditRepository.
func NewCreditRepository(db *gorm.DB) *CreditRepository {
	return &CreditRepository{db: db}
}

// Balance returns a student's current credit balance.
func (r *CreditRepository) Balance(studentID uint) (int, error) {
	var st model.Student
	if err := r.db.Select("credits").First(&st, studentID).Error; err != nil {
		return 0, err
	}
	return st.Credits, nil
}

// Debit atomically subtracts credits for an AI action and records a debit entry.
// It fails with ErrInsufficientCredits if the balance is too low (the conditional
// UPDATE guards against races and concurrent asks).
func (r *CreditRepository) Debit(studentID uint, credits int, action string, aiCostPaise int64, note string) (int, error) {
	var newBalance int
	err := r.db.Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&model.Student{}).
			Where("id = ? AND credits >= ?", studentID, credits).
			UpdateColumn("credits", gorm.Expr("credits - ?", credits))
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrInsufficientCredits
		}
		if err := tx.Create(&model.CreditLedger{
			StudentID:   studentID,
			Kind:        "debit",
			Action:      action,
			Credits:     -credits,
			AICostPaise: aiCostPaise,
			Note:        note,
		}).Error; err != nil {
			return err
		}
		var st model.Student
		if err := tx.Select("credits").First(&st, studentID).Error; err != nil {
			return err
		}
		newBalance = st.Credits
		return nil
	})
	return newBalance, err
}

// Grant adds credits (a plan grant or a paid recharge) and records the revenue.
func (r *CreditRepository) Grant(studentID uint, credits int, revenuePaise int64, kind, note string) (int, error) {
	var newBalance int
	err := r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.Student{}).
			Where("id = ?", studentID).
			UpdateColumn("credits", gorm.Expr("credits + ?", credits)).Error; err != nil {
			return err
		}
		if err := tx.Create(&model.CreditLedger{
			StudentID:    studentID,
			Kind:         kind,
			Credits:      credits,
			RevenuePaise: revenuePaise,
			Note:         note,
		}).Error; err != nil {
			return err
		}
		var st model.Student
		if err := tx.Select("credits").First(&st, studentID).Error; err != nil {
			return err
		}
		newBalance = st.Credits
		return nil
	})
	return newBalance, err
}

// PnL is the aggregate profit & loss across all credit activity.
type PnL struct {
	RevenuePaise int64 `json:"revenue_paise"`
	AICostPaise  int64 `json:"ai_cost_paise"`
	Debits       int64 `json:"debit_count"`
	Students     int64 `json:"paying_students"`
}

// Summary returns revenue vs AI cost for the admin profit & loss view.
func (r *CreditRepository) Summary() (*PnL, error) {
	var p PnL
	if err := r.db.Model(&model.CreditLedger{}).
		Select("COALESCE(SUM(revenue_paise),0)").Scan(&p.RevenuePaise).Error; err != nil {
		return nil, err
	}
	r.db.Model(&model.CreditLedger{}).Select("COALESCE(SUM(ai_cost_paise),0)").Scan(&p.AICostPaise)
	r.db.Model(&model.CreditLedger{}).Where("kind = ?", "debit").Count(&p.Debits)
	r.db.Model(&model.CreditLedger{}).Where("revenue_paise > 0").Distinct("student_id").Count(&p.Students)
	return &p, nil
}

// RevenueEntries returns every money-in ledger row (plan grants + recharges),
// newest first — the source for the admin payments report.
func (r *CreditRepository) RevenueEntries() ([]model.CreditLedger, error) {
	return r.RevenueEntriesFiltered(nil, nil, "")
}

// RevenueEntriesFiltered returns money-in ledger rows within an optional date
// range [from, to) and optional kind (grant | recharge), newest first.
func (r *CreditRepository) RevenueEntriesFiltered(from, to *time.Time, kind string) ([]model.CreditLedger, error) {
	q := r.db.Where("revenue_paise > 0")
	if from != nil {
		q = q.Where("created_at >= ?", *from)
	}
	if to != nil {
		q = q.Where("created_at < ?", *to)
	}
	if kind != "" {
		q = q.Where("kind = ?", kind)
	}
	var rows []model.CreditLedger
	err := q.Order("created_at DESC").Find(&rows).Error
	return rows, err
}

// PaymentsForStudent returns one student's money-in ledger rows (plan grants +
// recharges) — their own payment history — newest first.
func (r *CreditRepository) PaymentsForStudent(studentID uint, limit int) ([]model.CreditLedger, error) {
	if limit <= 0 {
		limit = 50
	}
	var rows []model.CreditLedger
	err := r.db.
		Where("student_id = ? AND revenue_paise > 0", studentID).
		Order("created_at DESC").
		Limit(limit).
		Find(&rows).Error
	return rows, err
}

// SummaryRange is Summary restricted to an optional date range [from, to).
func (r *CreditRepository) SummaryRange(from, to *time.Time) (*PnL, error) {
	base := func() *gorm.DB {
		q := r.db.Model(&model.CreditLedger{})
		if from != nil {
			q = q.Where("created_at >= ?", *from)
		}
		if to != nil {
			q = q.Where("created_at < ?", *to)
		}
		return q
	}
	var p PnL
	if err := base().Select("COALESCE(SUM(revenue_paise),0)").Scan(&p.RevenuePaise).Error; err != nil {
		return nil, err
	}
	base().Select("COALESCE(SUM(ai_cost_paise),0)").Scan(&p.AICostPaise)
	base().Where("kind = ?", "debit").Count(&p.Debits)
	base().Where("revenue_paise > 0").Distinct("student_id").Count(&p.Students)
	return &p, nil
}

// RecentByStudent returns a student's latest ledger entries (admin view).
func (r *CreditRepository) RecentByStudent(studentID uint, limit int) ([]model.CreditLedger, error) {
	var rows []model.CreditLedger
	err := r.db.Where("student_id = ?", studentID).
		Order("created_at DESC").Limit(limit).Find(&rows).Error
	return rows, err
}
