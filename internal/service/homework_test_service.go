package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
)

// TestQuestion is one generated written-test question.
type TestQuestion struct {
	N        int    `json:"n"`
	Question string `json:"question"`
	Marks    int    `json:"marks"`
}

// aiGrade is the JSON we ask Claude to return when grading.
type aiGrade struct {
	Summary string `json:"summary"`
	Results []struct {
		N        int    `json:"n"`
		Marks    int    `json:"marks"`
		Feedback string `json:"feedback"`
	} `json:"results"`
}

// GenerateTest asks the AI for a short written test on the homework, tailored to
// the student's class + language.
func (s *HomeworkService) GenerateTest(ctx context.Context, studentID, homeworkID uint) ([]TestQuestion, error) {
	hw, err := s.homeworks.GetForStudent(homeworkID, studentID)
	if err != nil {
		return nil, err
	}
	// Return the cached test if we already generated one for this homework.
	if strings.TrimSpace(hw.TestQuestions) != "" {
		var cached []TestQuestion
		if json.Unmarshal([]byte(hw.TestQuestions), &cached) == nil && len(cached) > 0 {
			return cached, nil
		}
	}
	st, err := s.students.FindByID(studentID)
	if err != nil {
		return nil, fmt.Errorf("student: %w", err)
	}
	var tasks strings.Builder
	for _, t := range hw.Tasks {
		fmt.Fprintf(&tasks, "- %s: %s\n", t.Title, t.Description)
	}
	system := "You are Vaha, a tutor setting a short written test for an Indian " +
		"school student. Reply with STRICT JSON only."
	prompt := fmt.Sprintf(`%s
Subject: %s
Homework: %s
Tasks:
%s
Create EXACTLY 5 short written-test questions covering this homework, suitable for
the student's class. Vary difficulty. Reply in %s. Return ONLY this JSON:
{"questions":[{"n":1,"question":"...","marks":2}]}`,
		profileLine(st), hw.Subject, hw.Title, tasks.String(), teachingLang(st))

	raw, err := s.chat.Complete(ctx, system, prompt)
	if err != nil {
		return nil, fmt.Errorf("generate test: %w", err)
	}
	var out struct {
		Questions []TestQuestion `json:"questions"`
	}
	if err := json.Unmarshal([]byte(extractJSON(raw)), &out); err != nil {
		return nil, fmt.Errorf("ai returned an unreadable test: %w", err)
	}
	// Normalise: sequential n, at least 1 mark each, cap 5.
	var qs []TestQuestion
	for i, q := range out.Questions {
		if i >= 5 {
			break
		}
		if q.Marks <= 0 {
			q.Marks = 1
		}
		q.N = i + 1
		q.Question = strings.TrimSpace(q.Question)
		if q.Question != "" {
			qs = append(qs, q)
		}
	}
	if len(qs) == 0 {
		return nil, fmt.Errorf("could not generate test questions")
	}
	if raw, err := json.Marshal(qs); err == nil {
		_ = s.homeworks.SaveTestQuestions(hw.ID, string(raw)) // cache for next time
	}
	return qs, nil
}

// GradeWritten grades text answers (typed, or speech-to-text transcripts for an
// oral test — set kind to "written" or "oral") and stores the result.
func (s *HomeworkService) GradeWritten(ctx context.Context, studentID, homeworkID uint, questions []TestQuestion, answers []string, kind string) (*model.HomeworkTest, error) {
	st, err := s.students.FindByID(studentID)
	if err != nil {
		return nil, fmt.Errorf("student: %w", err)
	}
	if _, err := s.homeworks.GetForStudent(homeworkID, studentID); err != nil {
		return nil, err // ownership check
	}
	var qa strings.Builder
	for i, q := range questions {
		ans := ""
		if i < len(answers) {
			ans = strings.TrimSpace(answers[i])
		}
		fmt.Fprintf(&qa, "Q%d (%d marks): %s\nStudent's answer: %s\n\n", q.N, q.Marks, q.Question, ans)
	}
	return s.grade(ctx, st, homeworkID, questions, qa.String(), "", "", kind)
}

// GradeWrittenImage grades a photo of the student's handwritten answer sheet
// (Claude vision reads the handwriting, including local languages).
func (s *HomeworkService) GradeWrittenImage(ctx context.Context, studentID, homeworkID uint, questions []TestQuestion, imageBytes []byte, mediaType string) (*model.HomeworkTest, error) {
	st, err := s.students.FindByID(studentID)
	if err != nil {
		return nil, fmt.Errorf("student: %w", err)
	}
	if _, err := s.homeworks.GetForStudent(homeworkID, studentID); err != nil {
		return nil, err
	}
	var qs strings.Builder
	for _, q := range questions {
		fmt.Fprintf(&qs, "Q%d (%d marks): %s\n", q.N, q.Marks, q.Question)
	}
	img := base64.StdEncoding.EncodeToString(imageBytes)
	return s.grade(ctx, st, homeworkID, questions, qs.String(), img, mediaType, "written")
}

// grade runs the grading prompt (text, or vision when img is set), builds the
// score, and persists the HomeworkTest. It returns the stored test.
func (s *HomeworkService) grade(ctx context.Context, st *model.Student, homeworkID uint, questions []TestQuestion, qaBlock, imgB64, mediaType, kind string) (*model.HomeworkTest, error) {
	if kind != "oral" {
		kind = "written"
	}
	maxScore := 0
	for _, q := range questions {
		maxScore += q.Marks
	}
	system := "You are Vaha, a fair, encouraging tutor grading a written test for an " +
		"Indian school student. Reply with STRICT JSON only."
	instruction := fmt.Sprintf(`%s
Grade each answer out of its marks. Be fair and give partial credit. Write brief,
kind feedback per question in %s. Return ONLY this JSON:
{"summary":"one or two lines on strengths + what to revise","results":[{"n":1,"marks":2,"feedback":"..."}]}

Questions%s:
%s`,
		profileLine(st), teachingLang(st),
		map[bool]string{true: " (the student's handwritten answers are in the image)", false: ""}[imgB64 != ""],
		qaBlock)

	var raw string
	var err error
	if imgB64 != "" {
		raw, err = s.chat.CompleteVision(ctx, system, instruction, imgB64, mediaType)
	} else {
		raw, err = s.chat.Complete(ctx, system, instruction)
	}
	if err != nil {
		return nil, fmt.Errorf("grade: %w", err)
	}
	var g aiGrade
	if err := json.Unmarshal([]byte(extractJSON(raw)), &g); err != nil {
		return nil, fmt.Errorf("ai returned an unreadable grade: %w", err)
	}

	// Score = sum of per-question marks, clamped to each question's max.
	markByN := map[int]int{}
	for _, q := range questions {
		markByN[q.N] = q.Marks
	}
	score := 0
	for i := range g.Results {
		if max, ok := markByN[g.Results[i].N]; ok && g.Results[i].Marks > max {
			g.Results[i].Marks = max
		}
		if g.Results[i].Marks < 0 {
			g.Results[i].Marks = 0
		}
		score += g.Results[i].Marks
	}
	detail, _ := json.Marshal(g.Results)

	test := &model.HomeworkTest{
		HomeworkID: homeworkID,
		StudentID:  st.ID,
		Kind:       kind,
		Score:      score,
		MaxScore:   maxScore,
		Summary:    strings.TrimSpace(g.Summary),
		Detail:     string(detail),
	}
	if err := s.homeworks.CreateTest(test); err != nil {
		return nil, fmt.Errorf("save test: %w", err)
	}
	return test, nil
}

// extractJSON pulls the first {...} object out of a model reply (tolerating code
// fences or stray text around it).
func extractJSON(raw string) string {
	s := strings.TrimSpace(raw)
	if i := strings.Index(s, "{"); i >= 0 {
		if j := strings.LastIndex(s, "}"); j > i {
			return s[i : j+1]
		}
	}
	return s
}

// teachingLang returns the student's teaching language, or a sensible default.
func teachingLang(st *model.Student) string {
	if v := strings.TrimSpace(st.TeachingLanguage); v != "" {
		return v
	}
	if v := strings.TrimSpace(st.Medium); v != "" {
		return v
	}
	return "simple English"
}
