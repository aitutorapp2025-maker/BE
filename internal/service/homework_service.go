package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/ai"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
)

// HomeworkService reads an uploaded homework photo with Claude vision, writes a
// friendly summary and splits it into a handful of learning tasks, persisting
// the result for the student.
type HomeworkService struct {
	homeworks  *repository.HomeworkRepository
	students   *repository.StudentRepository
	chat       *ai.Chat
	tutor      *TutorService
	uploadsDir string
	publicBase string
}

// NewHomeworkService builds a HomeworkService. uploadsDir is where images are
// written; publicBase is the URL prefix they're served from (/uploads). The
// tutor powers the "teach this task" step (RAG over the student's textbooks).
func NewHomeworkService(
	homeworks *repository.HomeworkRepository,
	students *repository.StudentRepository,
	chat *ai.Chat,
	tutor *TutorService,
	uploadsDir, publicBase string,
) *HomeworkService {
	return &HomeworkService{
		homeworks:  homeworks,
		students:   students,
		chat:       chat,
		tutor:      tutor,
		uploadsDir: uploadsDir,
		publicBase: publicBase,
	}
}

// TeachTask returns a short, grounded lesson for one task of a homework, scoped
// to the student (class/board/medium/language). The task's title + description
// is the topic taught.
func (s *HomeworkService) TeachTask(ctx context.Context, studentID, homeworkID, taskID uint) (*AskResult, error) {
	hw, err := s.homeworks.GetForStudent(homeworkID, studentID)
	if err != nil {
		return nil, err
	}
	var topic string
	for _, t := range hw.Tasks {
		if t.ID == taskID {
			topic = strings.TrimSpace(t.Title + ". " + t.Description)
			break
		}
	}
	if topic == "" {
		return nil, fmt.Errorf("task not found in this homework")
	}
	st, err := s.students.FindByID(studentID)
	if err != nil {
		return nil, fmt.Errorf("student: %w", err)
	}
	sc := StudentContext{
		Class:            st.StudentClass,
		Medium:           st.Medium,
		Board:            st.Board,
		Group:            st.StudentGroup,
		TeachingLanguage: st.TeachingLanguage,
	}
	return s.tutor.Teach(ctx, topic, sc)
}

// AskDoubt answers a follow-up question about a specific task, scoped to the
// student and grounded in their textbooks.
func (s *HomeworkService) AskDoubt(ctx context.Context, studentID, homeworkID, taskID uint, question string) (*AskResult, error) {
	hw, err := s.homeworks.GetForStudent(homeworkID, studentID)
	if err != nil {
		return nil, err
	}
	var topic string
	for _, t := range hw.Tasks {
		if t.ID == taskID {
			topic = strings.TrimSpace(t.Title + ". " + t.Description)
			break
		}
	}
	if topic == "" {
		return nil, fmt.Errorf("task not found in this homework")
	}
	st, err := s.students.FindByID(studentID)
	if err != nil {
		return nil, fmt.Errorf("student: %w", err)
	}
	sc := StudentContext{
		Class:            st.StudentClass,
		Medium:           st.Medium,
		Board:            st.Board,
		Group:            st.StudentGroup,
		TeachingLanguage: st.TeachingLanguage,
	}
	return s.tutor.AnswerDoubt(ctx, topic, question, sc)
}

// aiHomework is the JSON shape we ask Claude to return.
type aiHomework struct {
	Subject string `json:"subject"`
	Title   string `json:"title"`
	Summary string `json:"summary"`
	Tasks   []struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		DurationMin int    `json:"duration_min"`
	} `json:"tasks"`
}

// CreateFromImage saves the homework image, asks Claude vision to read it and
// split it into ~5 tasks, persists everything and returns the stored homework.
func (s *HomeworkService) CreateFromImage(ctx context.Context, studentID uint, imageBytes []byte, mediaType string) (*model.Homework, error) {
	if len(imageBytes) == 0 {
		return nil, fmt.Errorf("no image provided")
	}
	st, err := s.students.FindByID(studentID)
	if err != nil {
		return nil, fmt.Errorf("student: %w", err)
	}

	imageURL, err := s.saveImage(imageBytes, mediaType, studentID)
	if err != nil {
		return nil, fmt.Errorf("save image: %w", err)
	}

	system := "You are Vaha AI, a friendly tutor for Indian school students. You are " +
		"looking at a photo of a student's homework. Read it carefully and plan how " +
		"the student should learn it. Reply with STRICT JSON only — no markdown, no " +
		"prose outside the JSON."
	ctxLine := profileLine(st)
	prompt := ctxLine + "\n\nRead this homework image and return JSON with this exact shape:\n" +
		"{\n" +
		`  "subject": "the subject, e.g. Science",` + "\n" +
		`  "title": "a short homework title",` + "\n" +
		`  "summary": "one friendly line starting like: You have <subject> homework today — ...",` + "\n" +
		`  "tasks": [ {"title": "...", "description": "...", "duration_min": 10} ]` + "\n" +
		"}\n" +
		"Split the homework into EXACTLY 5 short tasks the student should do one by one " +
		"(read/understand, learn the concept, practise, revise, self-test). Keep each " +
		"title under 8 words and each description one short sentence. For each task set " +
		"duration_min: a realistic number of MINUTES (5–20) the student should spend on " +
		"it. Return ONLY the JSON."

	raw, err := s.chat.CompleteVision(ctx, system, prompt, base64.StdEncoding.EncodeToString(imageBytes), mediaType)
	if err != nil {
		return nil, fmt.Errorf("ai read homework: %w", err)
	}
	parsed, err := parseAIHomework(raw)
	if err != nil {
		return nil, fmt.Errorf("ai returned an unreadable plan: %w", err)
	}

	hw := &model.Homework{
		StudentID: studentID,
		Subject:   strings.TrimSpace(parsed.Subject),
		Title:     strings.TrimSpace(parsed.Title),
		Summary:   strings.TrimSpace(parsed.Summary),
		ImageURL:  imageURL,
		Status:    "new",
	}
	// Cap at 5 tasks; keep the model's order.
	for i, t := range parsed.Tasks {
		if i >= 5 {
			break
		}
		dur := t.DurationMin
		if dur < 1 || dur > 120 {
			dur = 10 // sane default when the AI omits/overshoots
		}
		hw.Tasks = append(hw.Tasks, model.HomeworkTask{
			OrderNo:     i + 1,
			Title:       strings.TrimSpace(t.Title),
			Description: strings.TrimSpace(t.Description),
			DurationMin: dur,
			Status:      "pending",
		})
	}
	if len(hw.Tasks) == 0 {
		return nil, fmt.Errorf("the AI could not break this homework into tasks — try a clearer photo")
	}
	if err := s.homeworks.Create(hw); err != nil {
		return nil, fmt.Errorf("save homework: %w", err)
	}
	return hw, nil
}

// List returns the student's homeworks (newest first).
func (s *HomeworkService) List(studentID uint) ([]model.Homework, error) {
	return s.homeworks.ListForStudent(studentID)
}

// Get returns one homework scoped to the student.
func (s *HomeworkService) Get(id, studentID uint) (*model.Homework, error) {
	return s.homeworks.GetForStudent(id, studentID)
}

// SetTaskStatus marks a task done/skipped/pending (skippable stages) and returns
// the refreshed homework with its recomputed status.
func (s *HomeworkService) SetTaskStatus(studentID, taskID uint, status string) (*model.Homework, error) {
	status = strings.TrimSpace(status)
	switch status {
	case "pending", "done", "skipped":
	default:
		return nil, fmt.Errorf("invalid status %q", status)
	}
	return s.homeworks.SetTaskStatus(taskID, studentID, status)
}

// HomeworkReport is one homework's line in the performance report.
type HomeworkReport struct {
	HomeworkID   uint      `json:"homework_id"`
	Title        string    `json:"title"`
	Subject      string    `json:"subject"`
	CreatedAt    time.Time `json:"created_at"`
	TasksTotal   int       `json:"tasks_total"`
	TasksDone    int       `json:"tasks_done"`
	TasksSkipped int       `json:"tasks_skipped"`
	TestsTaken   int       `json:"tests_taken"`
	Tested       bool      `json:"tested"`
	MarkPct      int       `json:"mark_pct"` // avg % of tests taken (0 if none)
}

// PerformanceReport aggregates a student's homework marks + progress, date-wise.
type PerformanceReport struct {
	Homeworks      []HomeworkReport `json:"homeworks"`
	TotalHomeworks int              `json:"total_homeworks"`
	TestedCount    int              `json:"tested_count"`
	OverallPct     int              `json:"overall_pct"` // avg mark over tested homeworks
}

// Report builds the student's performance report. Marks come only from tests the
// student actually took (skipped stages are excluded, never zeroed), matching the
// "average of completed stages only" policy. Homeworks are newest first.
func (s *HomeworkService) Report(studentID uint) (*PerformanceReport, error) {
	hws, err := s.homeworks.ListForStudent(studentID)
	if err != nil {
		return nil, err
	}
	out := &PerformanceReport{TotalHomeworks: len(hws)}
	markSum := 0
	for _, hw := range hws {
		line := HomeworkReport{
			HomeworkID: hw.ID,
			Title:      hw.Title,
			Subject:    hw.Subject,
			CreatedAt:  hw.CreatedAt,
			TasksTotal: len(hw.Tasks),
		}
		for _, t := range hw.Tasks {
			switch t.Status {
			case "done":
				line.TasksDone++
			case "skipped":
				line.TasksSkipped++
			}
		}
		tests, err := s.homeworks.TestsForHomework(hw.ID, studentID)
		if err != nil {
			return nil, err
		}
		if len(tests) > 0 {
			pctSum := 0
			for _, t := range tests {
				if t.MaxScore > 0 {
					pctSum += t.Score * 100 / t.MaxScore
				}
			}
			line.TestsTaken = len(tests)
			line.Tested = true
			line.MarkPct = pctSum / len(tests)
			out.TestedCount++
			markSum += line.MarkPct
		}
		out.Homeworks = append(out.Homeworks, line)
	}
	if out.TestedCount > 0 {
		out.OverallPct = markSum / out.TestedCount
	}
	return out, nil
}

// Sync returns the homeworks (with tasks) changed since `since` for local-first
// delta sync. A zero `since` returns the full history.
func (s *HomeworkService) Sync(studentID uint, since time.Time) ([]model.Homework, error) {
	return s.homeworks.ChangedForStudent(studentID, since)
}

// saveImage writes the image under uploadsDir/homework/ and returns its public URL.
func (s *HomeworkService) saveImage(data []byte, mediaType string, studentID uint) (string, error) {
	dir := filepath.Join(s.uploadsDir, "homework")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	ext := ".jpg"
	if strings.Contains(mediaType, "png") {
		ext = ".png"
	} else if strings.Contains(mediaType, "pdf") {
		ext = ".pdf"
	}
	name := fmt.Sprintf("hw-%d-%d%s", studentID, time.Now().UnixNano(), ext)
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		return "", err
	}
	base := strings.TrimRight(s.publicBase, "/")
	return base + "/uploads/homework/" + name, nil
}

// profileLine describes the student so the AI pitches the plan at the right level.
func profileLine(st *model.Student) string {
	parts := []string{}
	if st.StudentClass != "" {
		parts = append(parts, "class "+st.StudentClass)
	}
	if st.Board != "" {
		parts = append(parts, st.Board+" board")
	}
	if st.Medium != "" {
		parts = append(parts, st.Medium+" medium")
	}
	if len(parts) == 0 {
		return "The student's class is unknown."
	}
	return "The student is in " + strings.Join(parts, ", ") + "."
}

// parseAIHomework extracts the JSON object from Claude's reply (tolerating code
// fences or stray text around it) and unmarshals it.
func parseAIHomework(raw string) (*aiHomework, error) {
	s := strings.TrimSpace(raw)
	// Strip ```json … ``` fences if present.
	if i := strings.Index(s, "{"); i >= 0 {
		if j := strings.LastIndex(s, "}"); j > i {
			s = s[i : j+1]
		}
	}
	var out aiHomework
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil, err
	}
	return &out, nil
}
