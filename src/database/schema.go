package database

import (
	"time"
) 

type User struct {
	ID uint64 `gorm:"autoIncrement"`
	FirstName string `gorm:"type:char(20)"`
	LastName string `gorm:"type:char(50)"`
	Whatsapp uint64 
	Active bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

type WorkoutSet struct {
	ID uint64 `gorm:"autoIncrement"`
	UserId string `gorm:"type:char(20)"`
	Name string `gorm:"type:char(50)"`
	Whatsapp uint64 
	Active bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Message struct {
	ID uint64 `gorm:"autoIncrement"`
	UserId string `gorm:"type:char(20)"`
	LastName string `gorm:"type:char(50)"`
	Whatsapp uint64 
	Active bool
	CreatedAt time.Time
	 
}
