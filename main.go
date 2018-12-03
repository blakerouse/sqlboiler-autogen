package main

import (
	"database/sql"
	"fmt"
	"github.com/lib/pq"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"sqlboiler-autogen/local"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/volatiletech/sqlboiler/boilingcore"
	"github.com/volatiletech/sqlboiler/drivers"
	"github.com/volatiletech/sqlboiler/importers"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

const version = "0.1"

var (
	flagConfigFile string
	cmdState       *boilingcore.State
	cmdConfig      *boilingcore.Config
	rootCmd        *cobra.Command
)

func main() {
	// Set up the cobra root command
	rootCmd = &cobra.Command{
		Use:   "sqlboiler-autogen [flags]",
		Short: "Generate SQL Boiler ORM from your migrations.",
		Long: `Creates a local postgresql database, runs migrations, and then generates SQL Boiler ORM code.

SQL Boiler by default uses configuration files, this tool ignores all configuration files all options should
be passed as flags to this command.

Complete documentation of SQL Boiler is available at http://github.com/volatiletech/sqlboiler`,
		Example:       `sqlboiler-autogen`,
		RunE:          run,
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	// Set up the cobra root command flags
	rootCmd.PersistentFlags().StringP("migrations", "m", "./migrations", "The name of the folder to read migrations from")
	rootCmd.PersistentFlags().StringP("output", "o", "models", "The name of the folder to output to")
	rootCmd.PersistentFlags().StringP("pkgname", "p", "models", "The name you wish to assign to your generated package")
	rootCmd.PersistentFlags().StringSliceP("templates", "", nil, "A templates directory, overrides the bindata'd template folders in sqlboiler")
	rootCmd.PersistentFlags().StringSliceP("tag", "t", nil, "Struct tags to be included on your models in addition to json, yaml, toml")
	rootCmd.PersistentFlags().StringSliceP("replace", "", nil, "Replace templates by directory: relpath/to_file.tpl:relpath/to_replacement.tpl")
	rootCmd.PersistentFlags().BoolP("debug", "d", false, "Debug mode prints stack traces on error")
	rootCmd.PersistentFlags().BoolP("no-context", "", false, "Disable context.Context usage in the generated code")
	rootCmd.PersistentFlags().BoolP("no-tests", "", false, "Disable generated go test files")
	rootCmd.PersistentFlags().BoolP("no-hooks", "", false, "Disable hooks feature for your models")
	rootCmd.PersistentFlags().BoolP("no-rows-affected", "", false, "Disable rows affected in the generated API")
	rootCmd.PersistentFlags().BoolP("no-auto-timestamps", "", false, "Disable automatic timestamps for created_at/updated_at")
	rootCmd.PersistentFlags().BoolP("add-global-variants", "", false, "Enable generation for global variants")
	rootCmd.PersistentFlags().BoolP("add-panic-variants", "", false, "Enable generation for panic variants")
	rootCmd.PersistentFlags().BoolP("version", "", false, "Print the version")
	rootCmd.PersistentFlags().StringP("struct-tag-casing", "", "snake", "Decides the casing for go structure tag names. camel or snake (default snake)")

	// hide flags not recommended for use
	rootCmd.PersistentFlags().MarkHidden("replace")

	if err := rootCmd.Execute(); err != nil {
		if !getBoolP(rootCmd.PersistentFlags(), "debug") {
			fmt.Printf("Error: %v\n", err)
		} else {
			fmt.Printf("Error: %+v\n", err)
		}

		os.Exit(1)
	}
}

func run(cmd *cobra.Command, args []string) error {
	var err error

	// --version just prints the version and returns.
	flags := rootCmd.PersistentFlags()
	if getBoolP(flags, "version") {
		fmt.Println("sqlboiler-autogen v" + version)
		return nil
	}

	// Only support PostgreSQL with SQL Boiler.
	driverName := "psql"
	driverPath := "sqlboiler-psql"
	if p, err := exec.LookPath(driverPath); err == nil {
		driverPath = p
	}
	driverPath, err = filepath.Abs(driverPath)
	if err != nil {
		return errors.Wrap(err, "could not find absolute path to driver")
	}
	drivers.RegisterBinary(driverName, driverPath)

	// Ensure the ability for absolute path to migrations.
	migrationsPath := getStringP(flags, "migrations")
	migrationsPath, err = filepath.Abs(migrationsPath)
	if err != nil {
		return errors.Wrap(err, "could not find absolute path to migrations")
	}

	// Create the local database in a temp directory.
	tempDir, err := ioutil.TempDir(".", ".sqlboiler-autogen-")
	if err != nil {
		return errors.Wrap(err, "failed to create temporary directory")
	}
	defer os.RemoveAll(tempDir)
	db := local.NewDB(path.Join(tempDir, "db"))
	if err = db.Start(); err != nil {
		return errors.Wrap(err, "failed to start temporary postgresql")
	}
	defer db.Stop()

	// Load the migrations and connect to the database.
	m, err := migrate.New("file://"+migrationsPath, db.Name())
	if err != nil {
		return errors.Wrap(err, "failed to load migrations")
	}

	// Migrate all they way up.
	if err = m.Up(); err != nil {
		return errors.Wrap(err, "failed to migrate database")
	}

	// Get the configuration for the driver.
	driverConfig, err := getPsqlDriverConfig(db.Name())
	if err != nil {
		return errors.Wrap(err, "failed to create SQL Boiler driver config")
	}

	// Create the configurations from flags.
	cmdConfig = &boilingcore.Config{
		DriverName:       driverName,
		DriverConfig:     driverConfig,
		OutFolder:        getStringP(flags, "output"),
		PkgName:          getStringP(flags, "pkgname"),
		Debug:            getBoolP(flags, "debug"),
		AddGlobal:        getBoolP(flags, "add-global-variants"),
		AddPanic:         getBoolP(flags, "add-panic-variants"),
		NoContext:        getBoolP(flags, "no-context"),
		NoTests:          getBoolP(flags, "no-tests"),
		NoHooks:          getBoolP(flags, "no-hooks"),
		NoRowsAffected:   getBoolP(flags, "no-rows-affected"),
		NoAutoTimestamps: getBoolP(flags, "no-auto-timestamps"),
		Wipe:             true,                                                    // always wipe
		StructTagCasing:  strings.ToLower(getStringP(flags, "struct-tag-casing")), // camel | snake
		TemplateDirs:     getStringSliceP(flags, "templates"),
		Tags:             getStringSliceP(flags, "tag"),
		Replacements:     getStringSliceP(flags, "replace"),
		Imports:          importers.NewDefaultImports(),
	}

	// SQL Boiler requires a password to be set.
	//err = setPassword(db.Name(), driverConfig["user"].(string), "password")
	//if err != nil {
	//	return err
	//}
	//driverConfig["pass"] = "password"

	// Run SQL Boiler.
	cmdState, err = boilingcore.New(cmdConfig)
	if err != nil {
		return err
	}
	err = cmdState.Run()
	if err != nil {
		return err
	}
	return cmdState.Cleanup()
}

func getPsqlDriverConfig(connURL string) (map[string]interface{}, error) {
	config := map[string]interface{}{
		"blacklist": []string{"migrations"},
	}
	parsed, err := pq.ParseURL(connURL)
	if err != nil {
		return nil, err
	}
	pieces := strings.Split(parsed, " ")
	for _, piece := range pieces {
		parts := strings.Split(piece, "=")
		if parts[0] == "port" {
			port, err := strconv.Atoi(parts[1])
			if err != nil {
				// shouldn't happen
				panic(err)
			}
			config["port"] = port
		} else {
			config[parts[0]] = parts[1]
		}
	}
	return config, nil
}

func setPassword(connURL string, username string, password string) error {
	db, err := sql.Open("postgres", connURL)
	if err != nil {
		return errors.Wrap(err, "failed connection to postgresql")
	}
	_, err = db.Exec(
		"ALTER USER " + username + " WITH PASSWORD '" + password + "';")
	if err != nil {
		return errors.Wrap(err, "failed to set postgresql user password")
	}
	return nil
}

func getBoolP(p *pflag.FlagSet, key string) bool {
	value, err := p.GetBool(key)
	if err != nil {
		panic(err)
	}
	return value
}

func getStringP(p *pflag.FlagSet, key string) string {
	value, err := p.GetString(key)
	if err != nil {
		panic(err)
	}
	return value
}

func getStringSliceP(p *pflag.FlagSet, key string) []string {
	value, err := p.GetStringSlice(key)
	if err != nil {
		panic(err)
	}
	return value
}
