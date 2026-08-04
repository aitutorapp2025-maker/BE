package service

import (
	"crypto/rand"
	"errors"
	"fmt"
	"strings"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
)

// ReferralService runs the "refer & earn" program: it mints each student a
// unique share code, builds their share link + WhatsApp message from the
// admin-managed settings, and attributes a new signup to a referrer (accruing
// the referrer's next-bill discount).
type ReferralService struct {
	students  *repository.StudentRepository
	referrals *repository.ReferralRepository
	settings  *repository.SettingRepository
}

// NewReferralService builds a ReferralService.
func NewReferralService(
	students *repository.StudentRepository,
	referrals *repository.ReferralRepository,
	settings *repository.SettingRepository,
) *ReferralService {
	return &ReferralService{students: students, referrals: referrals, settings: settings}
}

// ReferralInfo is the student-facing view of their referral program state.
type ReferralInfo struct {
	Enabled      bool   `json:"enabled"`
	Code         string `json:"code"`
	Link         string `json:"link"`
	ShareMessage string `json:"share_message"` // ready-to-send text (link already substituted)
	RewardRupees int    `json:"reward_rupees"` // what the referrer earns per successful signup
	Count        int    `json:"count"`         // how many have joined via this student
	Pending      int    `json:"pending"`       // accrued discount waiting for the next bill
}

// defaultShareMessage is used when the admin hasn't set a template.
const defaultShareMessage = "I'm learning with Vaha AI — the AI tutor for classes 1–12. " +
	"Install the app with my link and start your free trial: {link}"

// MyReferral returns the signed-in student's referral info, generating their
// unique code on first access.
func (s *ReferralService) MyReferral(studentID uint) (*ReferralInfo, error) {
	set, err := s.settings.Get()
	if err != nil {
		return nil, err
	}
	st, err := s.students.FindByID(studentID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(st.ReferralCode) == "" {
		if err := s.ensureCode(st); err != nil {
			return nil, err
		}
	}
	link := s.buildLink(set, st.ReferralCode)
	tmpl := strings.TrimSpace(set.ReferralShareMessage)
	if tmpl == "" {
		tmpl = defaultShareMessage
	}
	msg := strings.NewReplacer("{link}", link, "{code}", st.ReferralCode).Replace(tmpl)
	return &ReferralInfo{
		Enabled:      set.ReferralEnabled,
		Code:         st.ReferralCode,
		Link:         link,
		ShareMessage: msg,
		RewardRupees: set.ReferralRewardRupees,
		Count:        st.ReferralCount,
		Pending:      st.ReferralRewardRupees,
	}, nil
}

// buildLink returns the student's share link: the admin-configured base + code.
// Falls back to a bare "/r/<code>" path if no base is set (still useful once the
// admin fills in the domain).
func (s *ReferralService) buildLink(set *model.Setting, code string) string {
	base := strings.TrimSpace(set.ReferralLinkBase)
	if base == "" {
		return "/r/" + code
	}
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	return base + code
}

// Attribute links a brand-new referee to the referrer that owns `code`. It is a
// best-effort, one-time operation guarded against self-referral and double
// attribution; on success it accrues the referrer's next-bill discount. Errors
// are returned for logging but should never block the signup itself.
func (s *ReferralService) Attribute(refereeID uint, code string) error {
	code = normalizeCode(code)
	if code == "" {
		return nil
	}
	set, err := s.settings.Get()
	if err != nil {
		return err
	}
	if !set.ReferralEnabled {
		return nil
	}
	referrer, err := s.referrals.FindStudentByCode(code)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil // unknown code — silently ignore
		}
		return err
	}
	if referrer.ID == refereeID {
		return nil // can't refer yourself
	}
	already, err := s.referrals.RefereeAlreadyReferred(refereeID)
	if err != nil {
		return err
	}
	if already {
		return nil // this account was already attributed once
	}
	referee, err := s.students.FindByID(refereeID)
	if err != nil {
		return err
	}

	reward := set.ReferralRewardRupees
	if err := s.referrals.Create(&model.Referral{
		ReferrerID:   referrer.ID,
		RefereeID:    referee.ID,
		Code:         code,
		RewardRupees: reward,
		ReferrerName: referrer.Name,
		RefereeName:  referee.Name,
	}); err != nil {
		return err
	}

	// Record who referred this student, and accrue the referrer's reward.
	referee.ReferredByID = referrer.ID
	_ = s.students.Update(referee)
	referrer.ReferralRewardRupees += reward
	referrer.ReferralCount++
	return s.students.Update(referrer)
}

// ensureCode generates a unique referral code for a student and persists it.
func (s *ReferralService) ensureCode(st *model.Student) error {
	for attempt := 0; attempt < 8; attempt++ {
		code, err := genReferralCode()
		if err != nil {
			return err
		}
		exists, err := s.referrals.CodeExists(code)
		if err != nil {
			return err
		}
		if exists {
			continue
		}
		st.ReferralCode = code
		if err := s.students.Update(st); err != nil {
			return err
		}
		return nil
	}
	return fmt.Errorf("could not generate a unique referral code")
}

// codeAlphabet excludes easily-confused characters (0/O, 1/I/L) so codes are
// safe to read aloud or type. 7 chars over 30 symbols ≈ 2.2e10 combinations.
const codeAlphabet = "23456789ABCDEFGHJKMNPQRSTUVWXYZ"

// genReferralCode returns a random 7-character referral code.
func genReferralCode() (string, error) {
	const n = 7
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	out := make([]byte, n)
	for i := range b {
		out[i] = codeAlphabet[int(b[i])%len(codeAlphabet)]
	}
	return string(out), nil
}

// normalizeCode upper-cases and trims a referral code and strips anything that
// isn't an allowed code character, so links/typing variations still resolve.
func normalizeCode(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	var b strings.Builder
	for _, r := range code {
		if strings.ContainsRune(codeAlphabet, r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}
