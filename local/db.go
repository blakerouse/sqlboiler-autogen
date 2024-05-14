package local

import (
	"os"
	"os/exec"

	"github.com/koron-go/pgctl"

	"github.com/phayes/freeport"
)

// DB provides a local running instance of postgres
type DB struct {
	server *pgctl.Server
}

// NewDB returns a new local postgresql database.
func NewDB(dataDir string) *DB {
	// Verify that pg_ctl can be found.
	_, err := exec.LookPath("pg_ctl")
	if err != nil {
		// Ensure the environment is correct to find the pg_ctl command.
		_, set := os.LookupEnv("POSTGRES_HOME")
		if !set {
			// TODO: Improve this to do a better search of setting up
			// POSTGRES_HOME paths. At the moment this is hard coded to
			// Ubuntu Bionic PostgreSQL.
			os.Setenv("POSTGRES_HOME", "/usr/lib/postgresql/14")
		}
	}

	return &DB{
		server: pgctl.NewServer(dataDir),
	}
}

// Start starts PostgreSQL.
func (db *DB) Start() error {
	port, err := freeport.GetFreePort()
	if err != nil {
		return err
	}

	options := pgctl.StartOptions{
		Port:      uint16(port),
		SocketDir: "",
	}

	if err = db.server.StartOptions(&options); err != nil {
		return err
	}
	return db.server.Start()
}

// Stop stops the running PostgreSQL.
func (db *DB) Stop() error {
	return db.server.Stop()
}

// IsRunning checks PostgreSQL server is running or not.
func (db *DB) IsRunning() bool {
	return db.server.IsRunning()
}

// Name returns data source name if server is running.
// Otherwise returns empty string.
func (db *DB) Name() string {
	return db.server.Name()
}
