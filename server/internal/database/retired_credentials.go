package database

import (
	"gorm.io/gorm"
)

// deleteRetiredSSHKnownHosts removes the standalone SSH known-hosts
// credentials. Known hosts now live on the SSH key row itself; the old rows
// are not migrated. A second call finds nothing and is a no-op.
func deleteRetiredSSHKnownHosts(db *gorm.DB) error {
	if db == nil || !db.Migrator().HasTable("credentials") {
		return nil
	}
	return db.Exec("DELETE FROM credentials WHERE purpose = ? AND secret_kind = ?", "ssh", "ssh_known_hosts").Error
}
