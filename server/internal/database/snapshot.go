package database

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/storageguard"
	mysqlcfg "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
)

// Snapshot requires an established offline maintenance fence. SQL includes raw
// opaque references and ACLs, never deserialized model defaults. The caller
// pairs this file with the verified object inventory and external secrets.
func Snapshot(ctx context.Context, db *gorm.DB, cfg config.DatabaseConfig, owner, destination string) (err error) {
	if err := storageguard.Check(ctx, db, owner); err != nil {
		return err
	}
	destination, err = filepath.Abs(destination)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		_ = file.Close()
		if err != nil {
			_ = os.Remove(destination)
		}
	}()
	if cfg.Driver == "mysql" {
		err = dumpMySQL(ctx, cfg.DSN, file)
	} else {
		// VACUUM INTO also includes committed WAL content. Copying just the .db
		// file would silently omit it. An exclusively-created empty file is an
		// accepted SQLite VACUUM destination.
		err = db.WithContext(ctx).Exec("VACUUM INTO ?", destination).Error
	}
	if err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	if err = storageguard.Check(ctx, db, owner); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(destination))
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()
	return dir.Sync()
}

func dumpMySQL(ctx context.Context, dsn string, output *os.File) error {
	cfg, err := mysqlcfg.ParseDSN(dsn)
	if err != nil {
		return errors.New("invalid MySQL backup configuration")
	}
	options, err := mysqlClientOptions(cfg)
	if err != nil {
		return err
	}
	private, err := os.CreateTemp(filepath.Dir(output.Name()), ".mysql-backup-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(private.Name()) }()
	if _, err = private.WriteString(options); err != nil {
		_ = private.Close()
		return err
	}
	if err = private.Close(); err != nil {
		return err
	}
	// Credentials exist only in the 0600 client file, never argv, logs, or a
	// subprocess environment. Native stderr may echo credentials or endpoints;
	// do not forward it. The native client must be Oracle MySQL 8.4+ mysqldump.
	command := exec.CommandContext(ctx, "mysqldump", "--defaults-file="+private.Name(),
		"--no-login-paths", "--single-transaction", "--quick", "--skip-lock-tables", "--hex-blob", "--no-tablespaces",
		"--set-gtid-purged=OFF", "--skip-add-locks", "--default-character-set=utf8mb4", cfg.DBName)
	command.Stdout = output
	command.Env = []string{"PATH=" + os.Getenv("PATH")}
	if err = command.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("MySQL snapshot failed; check mysqldump 8.4+ installation and backup permissions")
	}
	return nil
}

func mysqlClientOptions(cfg *mysqlcfg.Config) (string, error) {
	quote := func(s string) string {
		return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", `\r`, "\t", `\t`, "\x00", `\0`).Replace(s) + `"`
	}
	result := "[client]\nuser=" + quote(cfg.User) + "\npassword=" + quote(cfg.Passwd) + "\n"
	switch cfg.Net {
	case "unix":
		result += "protocol=SOCKET\nsocket=" + quote(cfg.Addr) + "\n"
	case "tcp", "":
		host, port, err := net.SplitHostPort(cfg.Addr)
		if err != nil {
			return "", errors.New("invalid MySQL TCP backup address")
		}
		result += "protocol=TCP\nhost=" + quote(host) + "\nport=" + quote(port) + "\n"
	default:
		return "", errors.New("unsupported MySQL backup transport")
	}
	switch cfg.TLSConfig {
	case "", "false":
		result += "ssl-mode=DISABLED\n"
	case "true":
		result += "ssl-mode=VERIFY_IDENTITY\n"
	case "skip-verify":
		result += "ssl-mode=REQUIRED\n"
	case "preferred":
		result += "ssl-mode=PREFERRED\n"
	default:
		return "", errors.New("custom MySQL TLS profiles need a native client backup configuration")
	}
	if cfg.DBName == "" || strings.HasPrefix(cfg.DBName, "-") || strings.ContainsAny(cfg.DBName, "\x00\n\r") {
		return "", fmt.Errorf("invalid MySQL backup database name")
	}
	return result, nil
}
