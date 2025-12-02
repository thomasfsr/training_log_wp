package database

import (
	"time"
) 

type User struct {
	ID uint32 `gorm:"autoIncrement"`
	FirstName string `gorm:"type:char(20)"`
	LastName string `gorm:"type:char(50)"`
	Whatsapp uint64 
	Active bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

type WorkoutSet struct {
	ID uint64 `gorm:"autoIncrement"`
	UserId uint32
	Exercise string `gorm:"type:char(100)"`
	Weight uint16 
	Reps uint8
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Message struct {
	ID uint64 `gorm:"autoIncrement"`
	UserId uint32
	Role string `gorm:"type:ENUM('user','assistant')"`
	Message string `gorm:"type:char(100)"`
	CreatedAt time.Time
}
