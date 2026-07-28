package service

import (
	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
)

// AI action identifiers charged against a student's credit balance.
const (
	ActionAskText      = "ask_text"
	ActionAskPhoto     = "ask_photo"
	ActionTimetable    = "timetable"
	ActionOralExam     = "oral_exam"
	ActionWrittenExam  = "written_exam"
	ActionParentReport = "parent_report"
)

// ActionCost is what one AI action costs. The invariant that guarantees ≥85%
// gross margin: **1 credit = ₹1 of AI-cost budget**, and Credits ≥ the real AI
// cost (we round the credit charge UP from the rupee cost). Plans then grant
// credits = 15% × price, so a student who burns every credit still costs ≤15%.
//
// AICostPaise is the *actual* estimated cost (for the admin P&L); Credits is
// what the student's balance drops by. Keeping Credits ≥ AICostPaise/100 keeps
// the margin floor intact.
type ActionCost struct {
	Credits     int   // charged to the student (ceil of the rupee cost)
	AICostPaise int64 // real estimated AI cost, for profit/loss
	Label       string
}

// actionCosts is the price list. Update these as real usage is measured; keep
// Credits ≥ ceil(AICostPaise/100) so the margin guarantee holds.
var actionCosts = map[string]ActionCost{
	ActionAskText:      {Credits: 2, AICostPaise: 150, Label: "Homework question"},
	ActionAskPhoto:     {Credits: 3, AICostPaise: 250, Label: "Photo/PDF question"},
	ActionTimetable:    {Credits: 1, AICostPaise: 40, Label: "Daily timetable"},
	ActionOralExam:     {Credits: 16, AICostPaise: 1600, Label: "Oral exam"},
	ActionWrittenExam:  {Credits: 6, AICostPaise: 600, Label: "Written exam"},
	ActionParentReport: {Credits: 2, AICostPaise: 200, Label: "Parent report"},
}

// CostOf returns the cost of an action (zero value if unknown).
func CostOf(action string) ActionCost { return actionCosts[action] }

// ActionCosts returns the full price list (admin view).
func ActionCosts() map[string]ActionCost { return actionCosts }

// SuggestedCredits returns the credit grant that yields the given margin for a
// plan price (default 85% → 15% of price as AI budget → that many credits,
// since 1 credit = ₹1 of budget). Used to prefill the plan form.
func SuggestedCredits(priceRupees int, marginPct int) int {
	if marginPct < 0 || marginPct > 99 {
		marginPct = 85
	}
	return priceRupees * (100 - marginPct) / 100
}

// CreditService enforces the credit balance around AI actions and records the
// ledger for the admin profit & loss.
type CreditService struct {
	repo *repository.CreditRepository
}

// NewCreditService builds a CreditService.
func NewCreditService(repo *repository.CreditRepository) *CreditService {
	return &CreditService{repo: repo}
}

// CanAfford reports whether the student has enough credits for the action.
func (s *CreditService) CanAfford(studentID uint, action string) (bool, int, error) {
	bal, err := s.repo.Balance(studentID)
	if err != nil {
		return false, 0, err
	}
	return bal >= CostOf(action).Credits, bal, nil
}

// Charge deducts the action's credits and records the debit (with its real AI
// cost). Returns repository.ErrInsufficientCredits if the balance is too low.
func (s *CreditService) Charge(studentID uint, action string) (newBalance int, err error) {
	c := CostOf(action)
	return s.repo.Debit(studentID, c.Credits, action, c.AICostPaise, c.Label)
}

// Grant adds credits from a plan or recharge, recording the revenue.
func (s *CreditService) Grant(studentID, credits int, revenuePaise int64, kind, note string) (int, error) {
	return s.repo.Grant(uint(studentID), credits, revenuePaise, kind, note)
}

// Balance returns the student's current credit balance.
func (s *CreditService) Balance(studentID uint) (int, error) {
	return s.repo.Balance(studentID)
}

// Summary returns the aggregate profit & loss (admin only).
func (s *CreditService) Summary() (*repository.PnL, error) {
	return s.repo.Summary()
}

// Recent returns a student's latest ledger entries (admin only).
func (s *CreditService) Recent(studentID uint, limit int) ([]model.CreditLedger, error) {
	return s.repo.RecentByStudent(studentID, limit)
}
