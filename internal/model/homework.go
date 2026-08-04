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
	Status      string    `gorm:"size:20;not null;default:pending" json:"status"` // pending | done
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// TableName sets the table name explicitly.
func (HomeworkTask) TableName() string { return "homework_tasks" }
