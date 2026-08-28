package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/ai"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/doctext"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/media"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
)

// HomeworkService reads an uploaded homework photo with Claude vision, writes a
// friendly summary and splits it into a handful of learning tasks, persisting
// the result for the student.
type HomeworkService struct {
	homeworks   *repository.HomeworkRepository
	students    *repository.StudentRepository
	chat        *ai.Chat
	tutor       *TutorService
	privateDir  string // homework images live here (NOT publicly served)
	publicBase  string
	mediaSecret string // signs the private-media access URLs
}

// NewHomeworkService builds a HomeworkService. privateDir is where homework
// images are written (served only via a signed /media route); publicBase is the
// URL base; mediaSecret signs the image URLs. The tutor powers the "teach" step.
func NewHomeworkService(
	homeworks *repository.HomeworkRepository,
	students *repository.StudentRepository,
	chat *ai.Chat,
	tutor *TutorService,
	privateDir, publicBase, mediaSecret string,
) *HomeworkService {
	return &HomeworkService{
		homeworks:   homeworks,
		students:    students,
		chat:        chat,
		tutor:       tutor,
		privateDir:  privateDir,
		publicBase:  publicBase,
		mediaSecret: mediaSecret,
	}
}

// TeachTask returns a short, grounded lesson for one task of a homework, scoped
// to the student (class/board/medium/language). The task's title + description
// is the topic taught.
// TeachTask returns a lesson for a task. It returns cached=true (and does no AI
// call) when the lesson was already generated, so re-opening a task is free.
func (s *HomeworkService) TeachTask(ctx context.Context, studentID, homeworkID, taskID uint) (*AskResult, bool, error) {
	hw, err := s.homeworks.GetForStudent(homeworkID, studentID)
	if err != nil {
		return nil, false, err
	}
	var topic, cached string
	for _, t := range hw.Tasks {
		if t.ID == taskID {
			topic = strings.TrimSpace(t.Title + ". " + t.Description)
			cached = t.Lesson
			break
		}
	}
	if topic == "" {
		return nil, false, fmt.Errorf("task not found in this homework")
	}
	if strings.TrimSpace(cached) != "" {
		return &AskResult{Answer: cached, Grounded: true}, true, nil
	}
	st, err := s.students.FindByID(studentID)
	if err != nil {
		return nil, false, fmt.Errorf("student: %w", err)
	}
	sc := StudentContext{
		Class:            st.StudentClass,
		Medium:           st.Medium,
		Board:            st.Board,
		Group:            st.StudentGroup,
		TeachingLanguage: st.TeachingLanguage,
	}
	res, err := s.tutor.Teach(ctx, topic, sc)
	if err != nil {
		return nil, false, err
	}
	_ = s.homeworks.SaveTaskLesson(taskID, res.Answer) // cache for next time
	return res, false, nil
}

// TeachTaskStream is the streaming variant of TeachTask. A cached lesson is
// streamed back in one delta (still free); a fresh lesson streams as it's
// generated and is cached afterwards. Returns (result, cached, error).
func (s *HomeworkService) TeachTaskStream(ctx context.Context, studentID, homeworkID, taskID uint, onDelta func(string)) (*AskResult, bool, error) {
	hw, err := s.homeworks.GetForStudent(homeworkID, studentID)
	if err != nil {
		return nil, false, err
	}
	var topic, cached string
	for _, t := range hw.Tasks {
		if t.ID == taskID {
			topic = strings.TrimSpace(t.Title + ". " + t.Description)
			cached = t.Lesson
			break
		}
	}
	if topic == "" {
		return nil, false, fmt.Errorf("task not found in this homework")
	}
	if strings.TrimSpace(cached) != "" {
		if onDelta != nil {
			onDelta(cached)
		}
		return &AskResult{Answer: cached, Grounded: true}, true, nil
	}
	st, err := s.students.FindByID(studentID)
	if err != nil {
		return nil, false, fmt.Errorf("student: %w", err)
	}
	sc := StudentContext{
		Class:            st.StudentClass,
		Medium:           st.Medium,
		Board:            st.Board,
		Group:            st.StudentGroup,
		TeachingLanguage: st.TeachingLanguage,
	}
	res, err := s.tutor.TeachStream(ctx, topic, sc, onDelta)
	if err != nil {
		return nil, false, err
	}
	_ = s.homeworks.SaveTaskLesson(taskID, res.Answer) // cache for next time
	return res, false, nil
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

// AskDoubtImage answers a doubt the student sent as a PHOTO during a learning
// session (a book/notebook question, possibly handwritten), scoped to a task.
func (s *HomeworkService) AskDoubtImage(ctx context.Context, studentID, homeworkID, taskID uint, question, imageB64, mediaType string) (*AskResult, error) {
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
	return s.tutor.AnswerDoubtImage(ctx, topic, question, imageB64, mediaType, sc)
}

// AskDoubtStream is the streaming variant of AskDoubt: it streams the answer via
// onDelta as it's generated and returns the full text once done.
func (s *HomeworkService) AskDoubtStream(ctx context.Context, studentID, homeworkID, taskID uint, question string, onDelta func(string)) (*AskResult, error) {
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
	return s.tutor.AnswerDoubtStream(ctx, topic, question, sc, onDelta)
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

// HomeworkAttachment is one uploaded homework file: a photo/PDF (read by the
// vision model) or a Word/PowerPoint/text document (text-extracted).
type HomeworkAttachment struct {
	Data      []byte
	MediaType string
}

// CreateFromImage keeps the single-file path (older app builds) — it wraps
// CreateFromAttachments.
func (s *HomeworkService) CreateFromImage(ctx context.Context, studentID uint, imageBytes []byte, mediaType, note string) (*model.Homework, error) {
	return s.CreateFromAttachments(ctx, studentID,
		[]HomeworkAttachment{{Data: imageBytes, MediaType: mediaType}}, note)
}

// CreateFromAttachments saves the homework files, has the AI read ALL of them
// together (multi-page photos + PDFs via vision; Word/PPT/text extracted to
// text) and split the homework into tasks. note is the student's own words
// (typed or spoken) — folded into the prompt so it shapes the task plan.
func (s *HomeworkService) CreateFromAttachments(ctx context.Context, studentID uint, atts []HomeworkAttachment, note string) (*model.Homework, error) {
	if len(atts) == 0 {
		return nil, fmt.Errorf("no files provided")
	}
	st, err := s.students.FindByID(studentID)
	if err != nil {
		return nil, fmt.Errorf("student: %w", err)
	}

	// Partition: photos/PDFs go to the vision model; documents become text.
	var vision []ai.VisionAttachment
	var docTexts []string
	imageURL := ""
	for _, a := range atts {
		if len(a.Data) == 0 {
			continue
		}
		mt := strings.ToLower(strings.TrimSpace(a.MediaType))
		switch {
		case mt == "image/jpeg" || mt == "image/jpg" || mt == "image/png" ||
			mt == "application/pdf":
			if mt == "image/jpg" {
				mt = "image/jpeg"
			}
			vision = append(vision, ai.VisionAttachment{
				B64: base64.StdEncoding.EncodeToString(a.Data), MediaType: mt})
			if imageURL == "" {
				if u, serr := s.saveImage(a.Data, mt, studentID); serr == nil {
					imageURL = u
				}
			}
		case doctext.Supported(mt):
			text, derr := doctext.Extract(a.Data, mt)
			if derr != nil {
				return nil, derr
			}
			docTexts = append(docTexts, text)
		default:
			return nil, fmt.Errorf("unsupported file type — please upload JPG/PNG photos, PDF, Word (.docx), PowerPoint (.pptx) or text files")
		}
	}
	if len(vision) == 0 && len(docTexts) == 0 {
		return nil, fmt.Errorf("no readable files provided")
	}

	system := "You are Vaha AI, a friendly tutor for Indian school students. You are " +
		"looking at a student's homework (photos, PDFs and/or document text). Read it " +
		"carefully and plan how the student should learn it. Reply with STRICT JSON " +
		"only — no markdown, no prose outside the JSON."
	ctxLine := profileLine(st)
	// The student's own words (typed or spoken) about this homework — e.g.
	// "focus on question 3, I don't understand fractions" — shape the plan.
	if n := strings.TrimSpace(note); n != "" {
		if len(n) > 1000 {
			n = n[:1000]
		}
		ctxLine += "\n\nThe student sent this message with the homework (typed or " +
			"spoken aloud): \"" + n + "\". Take it into account when reading the " +
			"homework and planning the tasks."
	}
	for i, t := range docTexts {
		ctxLine += fmt.Sprintf("\n\nContent of uploaded document %d:\n%s", i+1, t)
	}
	prompt := ctxLine + "\n\nRead this homework (all pages/files together are ONE homework) and return JSON with this exact shape:\n" +
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

	var raw string
	if len(vision) > 0 {
		raw, err = s.chat.CompleteVisionMulti(ctx, system, prompt, vision)
	} else {
		// Documents only (Word/PPT/text) — plain completion, no vision needed.
		raw, err = s.chat.Complete(ctx, system, prompt)
	}
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
	// Default timetable: the first task starts now (the student has the app
	// open, so its reminder is pre-marked sent); each next task is chained after
	// the previous one's duration plus a short break. The student can move any
	// task's time from the app, which re-arms that task's push reminder.
	now := time.Now()
	start := now
	for i := range hw.Tasks {
		at := start
		hw.Tasks[i].ScheduledAt = &at
		if i == 0 {
			sent := now
			hw.Tasks[i].ReminderSentAt = &sent
		}
		start = start.Add(time.Duration(hw.Tasks[i].DurationMin+5) * time.Minute)
	}
	if err := s.homeworks.Create(hw); err != nil {
		return nil, fmt.Errorf("save homework: %w", err)
	}
	return hw, nil
}

// RescheduleTask moves one task's study time (the student picked a new time in
// the app). The time must be within a sane window: not more than a few minutes
// in the past, and at most 30 days ahead.
func (s *HomeworkService) RescheduleTask(studentID, taskID uint, at time.Time) (*model.Homework, error) {
	now := time.Now()
	if at.Before(now.Add(-10 * time.Minute)) {
		return nil, fmt.Errorf("please pick a time that is not in the past")
	}
	if at.After(now.Add(30 * 24 * time.Hour)) {
		return nil, fmt.Errorf("please pick a time within the next 30 days")
	}
	return s.homeworks.RescheduleTask(taskID, studentID, at)
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
	// Load ALL of the student's tests once, then group by homework in memory
	// (avoids a query per homework).
	allTests, err := s.homeworks.TestsForStudent(studentID)
	if err != nil {
		return nil, err
	}
	testsByHw := map[uint][]model.HomeworkTest{}
	for _, t := range allTests {
		testsByHw[t.HomeworkID] = append(testsByHw[t.HomeworkID], t)
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
		tests := testsByHw[hw.ID]
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

// saveImage writes the image under privateDir/homework/ with a random,
// unguessable filename and returns a SIGNED URL (validated by the media route).
// The image is never on the public /uploads mount, so it can't be enumerated or
// fetched without the signature.
func (s *HomeworkService) saveImage(data []byte, mediaType string, _ uint) (string, error) {
	dir := filepath.Join(s.privateDir, "homework")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	ext := ".jpg"
	if strings.Contains(mediaType, "png") {
		ext = ".png"
	} else if strings.Contains(mediaType, "pdf") {
		ext = ".pdf"
	}
	rnd := make([]byte, 16)
	if _, err := rand.Read(rnd); err != nil {
		return "", err
	}
	name := hex.EncodeToString(rnd) + ext
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		return "", err
	}
	base := strings.TrimRight(s.publicBase, "/")
	return base + "/api/v1/media/hw/" + name + "?sig=" + media.Sign(name, s.mediaSecret), nil
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
