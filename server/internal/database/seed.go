package database

import (
	"gorm.io/gorm"
)

// Seed used to insert a sample gitlab-feature workflow on first boot. Fresh
// installs start with an empty workflow list; users create workflows themselves.
func Seed(db *gorm.DB) error {
	_ = db
	return nil
}
