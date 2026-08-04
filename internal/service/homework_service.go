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
	uploadsDir string
	publicBase string
}

// NewHomeworkService builds a HomeworkService. uploadsDir is where images are
// written; publicBase is the URL prefix they're served from (/uploads).
func NewHomeworkService(
	homeworks *repository.HomeworkRepository,
	students *repository.StudentRepository,
	chat *ai.Chat,
	uploadsDir, publicBase string,
) *HomeworkService {
	return &HomeworkService{
		homeworks:  homeworks,
		students:   students,
		chat:       chat,
		uploadsDir: uploadsDir,
		publicBase: publicBase,
	}
}

// aiHomework is the JSON shape we ask Claude to return.
type aiHomework struct {
	Subject string `json:"subject"`
	Title   string `json:"title"`
	Summary string `json:"summary"`
	Tasks   []struct {
		Title       string `json:"title"`
		Description string `json:"description"`
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
		`  "tasks": [ {"title": "...", "description": "..."} ]` + "\n" +
		"}\n" +
		"Split the homework into EXACTLY 5 short tasks the student should do one by one " +
		"(read/understand, learn the concept, practise, revise, self-test). Keep each " +
		"title under 8 words and each description one short sentence. Return ONLY the JSON."

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
		hw.Tasks = append(hw.Tasks, model.HomeworkTask{
			OrderNo:     i + 1,
			Title:       strings.TrimSpace(t.Title),
			Description: strings.TrimSpace(t.Description),
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
