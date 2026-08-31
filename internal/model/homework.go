package model

import "time"

// Homework is a homework the student uploaded (photo/PDF). The AI reads the
// image, writes a short summary ("you have this homework today…") and splits it
// into a handful of learning tasks the student works through one by one.
type Homework struct {
	ID        uint           `gorm:"primaryKey" json:"id"`
	// Covered by the composite idx_homeworks_student_created / _updated, so no
	// standalone index tag here (it would just be redundant write overhead).
	StudentID uint           `gorm:"not null" json:"student_id"`
	Subject   string         `gorm:"size:60" json:"subject"`
	Title     string         `gorm:"size:160" json:"title"`
	Summary   string         `gorm:"type:text" json:"summary"`
	// Difficulty is the AI's read of the homework (easy | medium | hard) and
	// FocusArea is the topic/skill the student should concentrate on — both
	// narrated by the AI-Teacher flow after analysis.
	Difficulty string `gorm:"size:20" json:"difficulty"`
	FocusArea  string `gorm:"size:300" json:"focus_area"`
	ImageURL  string         `gorm:"size:400" json:"image_url"`
	Status    string         `gorm:"size:20;not null;default:new" json:"status"` // new | in_progress | done
	// TestQuestions caches the AI-generated written/oral test (JSON) so re-taking
	// doesn't re-hit the AI. Not serialized to the client (used server-side).
	TestQuestions string `gorm:"type:text" json:"-"`
	Tasks     []HomeworkTask `gorm:"foreignKey:HomeworkID" json:"tasks"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

// TableName sets the table name explicitly.
func (Homework) TableName() string { return "homeworks" }

// HomeworkTask is one step the AI carved out of a homework. The student moves
// through each task (teach → clear doubts → oral test → written test in later
// phases); for now a task carries its title/description/order and a status.
type HomeworkTask struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	// Covered by the composite idx_hw_tasks_hw_order — no standalone index tag.
	HomeworkID  uint      `gorm:"not null" json:"homework_id"`
	OrderNo     int       `gorm:"not null;default:0" json:"order_no"`
	Title       string    `gorm:"size:200" json:"title"`
	Description string    `gorm:"type:text" json:"description"`
	// DurationMin is how long the AI suggests spending on this task; the app runs
	// a countdown and alerts the student when the time is up (timed-task mode).
	DurationMin int    `gorm:"not null;default:10" json:"duration_min"`
	// Lesson caches the AI "teach" output so re-opening a task is free + instant.
	// Not serialized to the client (the teach endpoint returns it explicitly).
	Lesson string `gorm:"type:text" json:"-"`
	Status string `gorm:"size:20;not null;default:pending" json:"status"` // pending | done
	// ScheduledAt is when the student should start this task. A default
	// timetable is chained at upload (task after task); the student can move any
	// task's time from the app, and a push reminder fires when the time arrives.
	ScheduledAt *time.Time `json:"scheduled_at"`
	// ReminderSentAt marks the "time to study" push as sent (nil = not yet), so
	// the minutely reminder job never notifies the same task twice.
	ReminderSentAt *time.Time `json:"-"`
	// StartedAt is stamped the first time the AI teaches this task and
	// CompletedAt when the student marks it done — together they give the
	// "how long did this task take" line in the learning history.
	StartedAt   *time.Time `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at"`
	// TimeupSentAt marks the "time's up" push as sent (fires when a STARTED
	// task's planned minutes run out and it still isn't done).
	TimeupSentAt *time.Time `json:"-"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// TableName sets the table name explicitly.
func (HomeworkTask) TableName() string { return "homework_tasks" }

// HomeworkTest is a graded test attempt on a homework (written or oral). Marks
// count only completed tests — a skipped test simply has no row. Detail holds
// the per-question breakdown as JSON so the report can show it.
type HomeworkTest struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	// HomeworkID is covered by the composite idx_hw_tests_hw_student; StudentID
	// keeps its own index (it's not the leading column of any composite).
	HomeworkID uint      `gorm:"not null" json:"homework_id"`
	StudentID  uint      `gorm:"index;not null" json:"student_id"`
	Kind       string    `gorm:"size:20;not null;default:written" json:"kind"` // written | oral
	Score      int       `gorm:"not null;default:0" json:"score"`
	MaxScore   int       `gorm:"not null;default:0" json:"max_score"`
	Summary    string    `gorm:"type:text" json:"summary"`
	Detail     string    `gorm:"type:text" json:"detail"` // JSON: per-question marks + feedback
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// TableName sets the table name explicitly.
func (HomeworkTest) TableName() string { return "homework_tests" }

// HomeworkDoubt is one doubt the student asked while a task was being taught,
// with the tutor's answer — kept so the learning history can replay exactly
// what was asked and what the teacher said.
type HomeworkDoubt struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	HomeworkID uint      `gorm:"index;not null" json:"homework_id"`
	TaskID     uint      `gorm:"index;not null" json:"task_id"`
	StudentID  uint      `gorm:"not null" json:"student_id"`
	Question   string    `gorm:"type:text" json:"question"`
	Answer     string    `gorm:"type:text" json:"answer"`
	CreatedAt  time.Time `json:"created_at"`
}

// TableName sets the table name explicitly.
func (HomeworkDoubt) TableName() string { return "homework_doubts" }
