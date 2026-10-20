// workspace-migrate is an offline operator tool. It deliberately opens an
// existing database directly, without the application's AutoMigrate or seed.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/cocofhu/grasp/internal/workspacemigration"
	"gorm.io/driver/mysql"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "workspace migration:", err)
		os.Exit(1)
	}
}

func run(args []string, out, diagnostic io.Writer) (result error) {
	if len(args) == 0 {
		return fmt.Errorf("usage: workspace-migrate plan|apply|verify|rollback --database PATH --profiles DIR --plan FILE [--backup DIR --offline]")
	}
	command := args[0]
	if command != "plan" && command != "apply" && command != "verify" && command != "rollback" {
		return fmt.Errorf("unknown command; use plan, apply, verify or rollback")
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(diagnostic)
	driver := flags.String("driver", "sqlite", "sqlite or mysql")
	database := flags.String("database", "", "existing SQLite database path")
	dsnEnv := flags.String("dsn-env", "GRASP_MIGRATION_DSN", "environment variable holding the MySQL DSN; never pass a secret on the command line")
	profiles := flags.String("profiles", "", "actual Agent profiles root, including when empty")
	shared := flags.String("shared", "", "shared baselines root; defaults beside profiles")
	canonical := flags.String("workspace", "", "existing ID to retain; required for multiple projects")
	planFile := flags.String("plan", "", "private JSON plan file")
	backup := flags.String("backup", "", "new, private backup directory outside live data")
	decisions := flags.String("decisions", "", "JSON decisions for renaming, orphan repair, tool authorization and pausing former PM jobs")
	offline := flags.Bool("offline", false, "acknowledge all writers stopped after draining sessions, scheduler, notifications and outbound queues")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() > 0 || *planFile == "" {
		return fmt.Errorf("a plan file is required and positional arguments are not accepted")
	}
	var plan workspacemigration.Plan
	if command != "plan" {
		data, err := os.ReadFile(*planFile)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(data, &plan); err != nil {
			return fmt.Errorf("invalid plan JSON")
		}
	}
	if command == "apply" || command == "rollback" {
		if !*offline || *backup == "" {
			return fmt.Errorf("--offline and --backup are required for writes")
		}
	}
	if command == "verify" && *backup == "" {
		return fmt.Errorf("--backup is required for verification")
	}
	if *driver == "sqlite" {
		roots := []string{*profiles, *shared, *backup}
		if command != "plan" {
			roots = []string{plan.Options.ProfilesRoot, plan.Options.SharedRoot, *backup}
		}
		if roots[1] == "" && roots[0] != "" {
			roots[1] = filepath.Join(filepath.Dir(roots[0]), "project-shared")
		}
		if err := validateDatabaseLocation(*database, roots); err != nil {
			return err
		}
	}
	readOnly := command == "plan" || command == "verify"
	db, err := openDatabase(*driver, *database, *dsnEnv, readOnly)
	if err != nil {
		return err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, sqlDB.Close()) }()
	switch command {
	case "plan":
		opts := workspacemigration.Options{CanonicalID: *canonical, ProfilesRoot: *profiles, SharedRoot: *shared}
		if *decisions != "" {
			data, err := os.ReadFile(*decisions)
			if err != nil {
				return err
			}
			if err := json.Unmarshal(data, &opts.Decisions); err != nil {
				return fmt.Errorf("invalid decisions JSON")
			}
		}
		plan, err = workspacemigration.BuildPlan(db, opts)
		if err != nil {
			return err
		}
		if err := savePlan(*planFile, plan); err != nil {
			return err
		}
		_, err = fmt.Fprintf(out, "Plan written: %d source workspaces, %d blockers, %d workflow renames, %d memory renames, %d credential renames. No business data was modified.\n", len(plan.Projects), len(plan.Blockers), len(plan.WorkflowRenames), len(plan.MemoryRenames), len(plan.CredentialRenames))
		if err != nil {
			return err
		}
		if len(plan.Blockers) > 0 {
			return workspacemigration.ErrBlocked
		}
	case "apply":
		if err := workspacemigration.Apply(db, plan, *backup, *offline); err != nil {
			return err
		}
		_, err = fmt.Fprintln(out, "Workspace migration and integrity verification complete. Backups and journal retained.")
		if err != nil {
			return err
		}
	case "verify":
		if err := workspacemigration.Verify(db, plan, *backup); err != nil {
			return err
		}
		_, err = fmt.Fprintln(out, "Workspace, history and file fingerprints verified.")
		if err != nil {
			return err
		}
	case "rollback":
		if err := workspacemigration.Rollback(db, plan, *backup, *offline); err != nil {
			return err
		}
		_, err = fmt.Fprintln(out, "Source data restored. Restart the source application before accepting new writes.")
		if err != nil {
			return err
		}
	}
	return nil
}

func validateDatabaseLocation(database string, roots []string) error {
	database, err := workspacemigration.CanonicalPath(database)
	if err != nil {
		return err
	}
	for _, root := range roots {
		if root == "" {
			continue
		}
		absolute, err := workspacemigration.CanonicalPath(root)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(absolute, database)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("SQLite database must be outside profile, shared and backup trees")
		}
	}
	return nil
}

func openDatabase(driver, path, dsnEnv string, readOnly bool) (*gorm.DB, error) {
	var dialector gorm.Dialector
	switch driver {
	case "sqlite":
		if path == "" {
			return nil, fmt.Errorf("--database must identify an existing SQLite file")
		}
		absolute, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(absolute)
		if err != nil || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("SQLite database must be an existing regular file")
		}
		uri := url.URL{Scheme: "file", Path: absolute}
		query := url.Values{"_busy_timeout": {"5000"}}
		if readOnly {
			query.Set("mode", "ro")
		} else {
			query.Set("mode", "rw")
		}
		uri.RawQuery = query.Encode()
		dialector = sqlite.Open(uri.String())
	case "mysql":
		dsn := strings.TrimSpace(os.Getenv(dsnEnv))
		if dsn == "" {
			return nil, fmt.Errorf("the MySQL DSN environment variable is empty")
		}
		dialector = mysql.Open(dsn)
	default:
		return nil, fmt.Errorf("unsupported database driver")
	}
	db, err := gorm.Open(dialector, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return nil, err
	}
	connection, err := db.DB()
	if err != nil {
		return nil, err
	}
	connection.SetMaxOpenConns(1)
	return db, nil
}

func savePlan(path string, plan workspacemigration.Plan) error {
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(data)
	return errors.Join(writeErr, f.Sync(), f.Close())
}
