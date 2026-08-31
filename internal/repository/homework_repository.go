package repository

import (
	"errors"
	"time"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"gorm.io/gorm"
)

// HomeworkRepository provides data access for homeworks and their tasks.
type HomeworkRepository struct {
	db *gorm.DB
}

// NewHomeworkRepository builds a HomeworkRepository.
func NewHomeworkRepository(db *gorm.DB) *HomeworkRepository {
	return &HomeworkRepository{db: db}
}

// Create inserts a homework together with its tasks (GORM cascades the slice).
func (r *HomeworkRepository) Create(hw *model.Homework) error {
	return r.db.Create(hw).Error
}

// GetForStudent returns a homework (with tasks, ordered) scoped to the student,
// so one student can never read another's homework.
func (r *HomeworkRepository) GetForStudent(id, studentID uint) (*model.Homework, error) {
	var hw model.Homework
	err := r.db.
		Preload("Tasks", func(db *gorm.DB) *gorm.DB { return db.Order("order_no ASC") }).
		Where("id = ? AND student_id = ?", id, studentID).
		First(&hw).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &hw, nil
}

// SaveTaskLesson caches the AI lesson for a task.
func (r *HomeworkRepository) SaveTaskLesson(taskID uint, lesson string) error {
	return r.db.Model(&model.HomeworkTask{}).Where("id = ?", taskID).
		Update("lesson", lesson).Error
}

// DueTimeUps returns started, still-pending tasks whose planned minutes have
// run out and that haven't had the "time's up" push yet. Candidates are
// filtered by duration in Go (portable across SQL dialects); tasks started
// more than 3 hours ago are considered stale and skipped.
func (r *HomeworkRepository) DueTimeUps(now time.Time) ([]DueTaskReminder, error) {
	type row struct {
		TaskID      uint
		StudentID   uint
		TaskTitle   string
		Subject     string
		StartedAt   time.Time
		DurationMin int
	}
	var rows []row
	err := r.db.Model(&model.HomeworkTask{}).
		Select("homework_tasks.id AS task_id, homeworks.student_id, homework_tasks.title AS task_title, homeworks.subject, homework_tasks.started_at, homework_tasks.duration_min").
		Joins("JOIN homeworks ON homeworks.id = homework_tasks.homework_id").
		Where("homework_tasks.status = 'pending'").
		Where("homework_tasks.timeup_sent_at IS NULL").
		Where("homework_tasks.started_at IS NOT NULL AND homework_tasks.started_at > ?",
			now.Add(-3*time.Hour)).
		Where("homeworks.status <> 'done'").
		Limit(500).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]DueTaskReminder, 0, len(rows))
	for _, x := range rows {
		mins := x.DurationMin
		if mins <= 0 {
			mins = 10
		}
		if !now.Before(x.StartedAt.Add(time.Duration(mins) * time.Minute)) {
			out = append(out, DueTaskReminder{
				TaskID: x.TaskID, StudentID: x.StudentID,
				TaskTitle: x.TaskTitle, Subject: x.Subject,
			})
		}
	}
	return out, nil
}

// MarkTimeupsSent stamps timeup_sent_at so each task's push fires only once.
func (r *HomeworkRepository) MarkTimeupsSent(taskIDs []uint, now time.Time) error {
	if len(taskIDs) == 0 {
		return nil
	}
	return r.db.Model(&model.HomeworkTask{}).
		Where("id IN ?", taskIDs).
		Update("timeup_sent_at", now).Error
}

// MarkTaskStarted stamps started_at on a task the FIRST time it is taught
// (later teaches keep the original start, so "time taken" stays honest).
func (r *HomeworkRepository) MarkTaskStarted(taskID uint) error {
	return r.db.Model(&model.HomeworkTask{}).
		Where("id = ? AND started_at IS NULL", taskID).
		Update("started_at", time.Now()).Error
}

// CreateDoubt saves one asked doubt + its answer for the learning history.
func (r *HomeworkRepository) CreateDoubt(d *model.HomeworkDoubt) error {
	return r.db.Create(d).Error
}

// DoubtsForTask returns a task's doubts (oldest first), scoped to the student.
func (r *HomeworkRepository) DoubtsForTask(taskID, studentID uint) ([]model.HomeworkDoubt, error) {
	var out []model.HomeworkDoubt
	err := r.db.
		Where("task_id = ? AND student_id = ?", taskID, studentID).
		Order("created_at ASC").
		Find(&out).Error
	return out, err
}

// SaveTestQuestions caches the generated test JSON on a homework.
func (r *HomeworkRepository) SaveTestQuestions(homeworkID uint, questions string) error {
	return r.db.Model(&model.Homework{}).Where("id = ?", homeworkID).
		Update("test_questions", questions).Error
}

// CreateTest saves a graded test attempt.
func (r *HomeworkRepository) CreateTest(t *model.HomeworkTest) error {
	return r.db.Create(t).Error
}

// TestsForHomework returns the test attempts for a homework, scoped to the
// student (newest first) — used by the marks/report.
func (r *HomeworkRepository) TestsForHomework(homeworkID, studentID uint) ([]model.HomeworkTest, error) {
	var out []model.HomeworkTest
	err := r.db.
		Where("homework_id = ? AND student_id = ?", homeworkID, studentID).
		Order("created_at DESC").
		Find(&out).Error
	return out, err
}

// TestsForStudent returns ALL of a student's test attempts (across homeworks) in
// one query, so the report can group them in memory instead of running a query
// per homework (avoids an N+1).
func (r *HomeworkRepository) TestsForStudent(studentID uint) ([]model.HomeworkTest, error) {
	var out []model.HomeworkTest
	err := r.db.Where("student_id = ?", studentID).Find(&out).Error
	return out, err
}

// SetTaskStatus updates one task's status (pending|done|skipped), scoped to the
// student via the parent homework, then recomputes the homework's own status
// (done when every task is done/skipped, in_progress once any task is acted on)
// and touches its updated_at so the change flows through delta sync. Returns the
// refreshed homework with its tasks.
func (r *HomeworkRepository) SetTaskStatus(taskID, studentID uint, status string) (*model.Homework, error) {
	var task model.HomeworkTask
	err := r.db.
		Joins("JOIN homeworks ON homeworks.id = homework_tasks.homework_id").
		Where("homework_tasks.id = ? AND homeworks.student_id = ?", taskID, studentID).
		First(&task).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	updates := map[string]any{"status": status}
	// Stamp the completion time for the learning history ("this task took N
	// minutes"); reopening a task clears it.
	if status == "done" {
		updates["completed_at"] = time.Now()
	} else {
		updates["completed_at"] = nil
	}
	if err := r.db.Model(&model.HomeworkTask{}).
		Where("id = ?", task.ID).
		Updates(updates).Error; err != nil {
		return nil, err
	}

	// Recompute the parent homework's status from all its tasks.
	var tasks []model.HomeworkTask
	if err := r.db.Where("homework_id = ?", task.HomeworkID).Find(&tasks).Error; err != nil {
		return nil, err
	}
	hwStatus := "new"
	if len(tasks) > 0 {
		allSettled, anyActed := true, false
		for _, t := range tasks {
			if t.Status == "pending" {
				allSettled = false
			} else {
				anyActed = true
			}
		}
		switch {
		case allSettled:
			hwStatus = "done"
		case anyActed:
			hwStatus = "in_progress"
		}
	}
	if err := r.db.Model(&model.Homework{}).
		Where("id = ?", task.HomeworkID).
		Updates(map[string]any{"status": hwStatus, "updated_at": time.Now()}).Error; err != nil {
		return nil, err
	}
	return r.GetForStudent(task.HomeworkID, studentID)
}

// ChangedForStudent returns the student's homeworks (with tasks) whose updated_at
// is newer than `since` — the delta the app pulls to keep its local DB in sync.
// A zero `since` returns everything (first sync / after the user clears data).
func (r *HomeworkRepository) ChangedForStudent(studentID uint, since time.Time) ([]model.Homework, error) {
	var out []model.Homework
	q := r.db.
		Preload("Tasks", func(db *gorm.DB) *gorm.DB { return db.Order("order_no ASC") }).
		Where("student_id = ?", studentID)
	if !since.IsZero() {
		q = q.Where("updated_at > ?", since)
	}
	err := q.Order("created_at DESC").Find(&out).Error
	return out, err
}

// ListForStudent returns the student's homeworks (newest first) with their tasks.
func (r *HomeworkRepository) ListForStudent(studentID uint) ([]model.Homework, error) {
	var out []model.Homework
	err := r.db.
		Preload("Tasks", func(db *gorm.DB) *gorm.DB { return db.Order("order_no ASC") }).
		Where("student_id = ?", studentID).
		Order("created_at DESC").
		Find(&out).Error
	return out, err
}

// RescheduleTask moves one pending task's scheduled time (student-scoped via the
// parent homework) and re-arms its reminder, then returns the refreshed homework.
func (r *HomeworkRepository) RescheduleTask(taskID, studentID uint, at time.Time) (*model.Homework, error) {
	var task model.HomeworkTask
	err := r.db.
		Joins("JOIN homeworks ON homeworks.id = homework_tasks.homework_id").
		Where("homework_tasks.id = ? AND homeworks.student_id = ?", taskID, studentID).
		First(&task).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	// A time in the future re-arms the "time to study" reminder; a time already
	// past stays quiet (no point pushing about it).
	updates := map[string]any{"scheduled_at": at, "reminder_sent_at": nil}
	if !at.After(time.Now()) {
		updates["reminder_sent_at"] = time.Now()
	}
	if err := r.db.Model(&model.HomeworkTask{}).
		Where("id = ?", task.ID).Updates(updates).Error; err != nil {
		return nil, err
	}
	// Touch the homework so the change flows through delta sync to the app.
	if err := r.db.Model(&model.Homework{}).Where("id = ?", task.HomeworkID).
		Update("updated_at", time.Now()).Error; err != nil {
		return nil, err
	}
	return r.GetForStudent(task.HomeworkID, studentID)
}

// DueTaskReminder is one "time to study" push waiting to be sent.
type DueTaskReminder struct {
	TaskID    uint
	StudentID uint
	TaskTitle string
	Subject   string
}

// DueTaskReminders returns pending tasks whose scheduled time has arrived within
// the last 30 minutes and whose reminder was not yet sent. The 30-minute floor
// keeps a long FCM outage from flooding students with ancient reminders later.
func (r *HomeworkRepository) DueTaskReminders(now time.Time) ([]DueTaskReminder, error) {
	var out []DueTaskReminder
	err := r.db.Model(&model.HomeworkTask{}).
		Select("homework_tasks.id AS task_id, homeworks.student_id, homework_tasks.title AS task_title, homeworks.subject").
		Joins("JOIN homeworks ON homeworks.id = homework_tasks.homework_id").
		Where("homework_tasks.status = 'pending'").
		Where("homework_tasks.reminder_sent_at IS NULL").
		Where("homework_tasks.scheduled_at IS NOT NULL AND homework_tasks.scheduled_at <= ? AND homework_tasks.scheduled_at > ?",
			now, now.Add(-30*time.Minute)).
		Where("homeworks.status <> 'done'").
		Limit(500).
		Scan(&out).Error
	return out, err
}

// MarkRemindersSent stamps reminder_sent_at so the same task is never pushed twice.
func (r *HomeworkRepository) MarkRemindersSent(taskIDs []uint, now time.Time) error {
	if len(taskIDs) == 0 {
		return nil
	}
	return r.db.Model(&model.HomeworkTask{}).
		Where("id IN ?", taskIDs).
		Update("reminder_sent_at", now).Error
}

// DailySummary is one student's activity for a day — the basis of the parents'
// evening WhatsApp report.
type DailySummary struct {
	StudentID  uint
	TasksDone  int
	TestsTaken int
	Score      int
	MaxScore   int
}

// DailySummaries aggregates every student's activity in [from, to): homework
// tasks marked done and tests taken (with their combined score). Students with
// no activity in the window simply have no entry.
func (r *HomeworkRepository) DailySummaries(from, to time.Time) (map[uint]*DailySummary, error) {
	out := map[uint]*DailySummary{}
	get := func(id uint) *DailySummary {
		if s, ok := out[id]; ok {
			return s
		}
		s := &DailySummary{StudentID: id}
		out[id] = s
		return s
	}

	var tasks []struct {
		StudentID uint
		N         int
	}
	if err := r.db.Model(&model.HomeworkTask{}).
		Select("homeworks.student_id, COUNT(*) AS n").
		Joins("JOIN homeworks ON homeworks.id = homework_tasks.homework_id").
		Where("homework_tasks.status = 'done' AND homework_tasks.updated_at >= ? AND homework_tasks.updated_at < ?", from, to).
		Group("homeworks.student_id").
		Scan(&tasks).Error; err != nil {
		return nil, err
	}
	for _, t := range tasks {
		get(t.StudentID).TasksDone = t.N
	}

	var tests []struct {
		StudentID uint
		N         int
		Score     int
		MaxScore  int
	}
	if err := r.db.Model(&model.HomeworkTest{}).
		Select("student_id, COUNT(*) AS n, COALESCE(SUM(score),0) AS score, COALESCE(SUM(max_score),0) AS max_score").
		Where("created_at >= ? AND created_at < ?", from, to).
		Group("student_id").
		Scan(&tests).Error; err != nil {
		return nil, err
	}
	for _, t := range tests {
		s := get(t.StudentID)
		s.TestsTaken, s.Score, s.MaxScore = t.N, t.Score, t.MaxScore
	}
	return out, nil
}
