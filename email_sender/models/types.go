package models

import "time"

type Report struct {
	Seq       int
	Latitude  float64
	Longitude float64
	Team      int
	TS        time.Time
	Image     []byte
	ActionID  string
	Sent      bool
}

type AreaReports struct {
	AreaID       int
	ContactEmail string
	Reports      []Report
}

type Config struct {
	DBUser     string
	DBPassword string
	DBHost     string
	DBPort     string
	DBName     string
}

type Contact struct {
	AreaID        int
	Email         string
	ConsentReport bool
}
