package model

import "time"

// Homework is a homework the student uploaded (photo/PDF). The AI reads the
// image, writes a short summary ("you have this homework today…") and splits it
// into a handful of learning tasks the student works through one by one.
type Homework struct {
	ID        uint           `gorm:"primaryKey" json:"id"`
	StudentID uint           `gorm:"index;not null" json:"student_id"`
	Subject   string         `gorm:"size:60" json:"subject"`
	Title     string         `gorm:"size:160" json:"title"`
	Summary   string         `gorm:"type:text" json:"summary"`
	ImageURL  string         `gorm:"size:400" json:"image_url"`
	Status    string         `gorm:"size:20;not null;default:new" json:"status"` // new | in_progress | done
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
	HomeworkID  uint      `gorm:"index;not null" json:"homework_id"`
	OrderNo     int       `gorm:"not null;default:0" json:"order_no"`
	Title       string    `gorm:"size:200" json:"title"`
	Description string    `gorm:"type:text" json:"description"`
	// DurationMin is how long the AI suggests spending on this task; the app runs
	// a countdown and alerts the student when the time is up (timed-task mode).
	DurationMin int    `gorm:"not null;default:10" json:"duration_min"`
	Status      string `gorm:"size:20;not null;default:pending" json:"status"` // pending | done
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
	HomeworkID uint      `gorm:"index;not null" json:"homework_id"`
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
