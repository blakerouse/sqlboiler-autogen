// Package main defines a command line interface for the sqlboiler package
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/friendsofgo/errors"
	"github.com/lib/pq"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/volatiletech/sqlboiler/v4/boilingcore"
	"github.com/volatiletech/sqlboiler/v4/drivers"
	"github.com/volatiletech/sqlboiler/v4/importers"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"

	"github.com/blakerouse/sqlboiler-autogen/local"
)

const sqlBoilerVersion = "4.16.2"

var (
	flagConfigFile string
	cmdState       *boilingcore.State
	cmdConfig      *boilingcore.Config
)

func initConfig() {
	if len(flagConfigFile) != 0 {
		viper.SetConfigFile(flagConfigFile)
		if err := viper.ReadInConfig(); err != nil {
			fmt.Println("Can't read config:", err)
			os.Exit(1)
		}
		return
	}

	var err error
	viper.SetConfigName("sqlboiler")

	configHome := os.Getenv("XDG_CONFIG_HOME")
	homePath := os.Getenv("HOME")
	wd, err := os.Getwd()
	if err != nil {
		wd = "."
	}

	configPaths := []string{wd}
	if len(configHome) > 0 {
		configPaths = append(configPaths, filepath.Join(configHome, "sqlboiler"))
	} else {
		configPaths = append(configPaths, filepath.Join(homePath, ".config/sqlboiler"))
	}

	for _, p := range configPaths {
		viper.AddConfigPath(p)
	}

	// Ignore errors here, fallback to other validation methods.
	// Users can use environment variables if a config is not found.
	_ = viper.ReadInConfig()
}

func main() {
	// Too much happens between here and cobra's argument handling, for
	// something so simple just do it immediately.
	for _, arg := range os.Args {
		if arg == "--version" {
			fmt.Println("SQLBoiler v" + sqlBoilerVersion)
			return
		}
	}

	// Set up the cobra root command
	rootCmd := &cobra.Command{
		Use:   "sqlboiler-autogen [flags]",
		Short: "Generate SQL Boiler ORM from your migrations.",
		Long: `Creates a local postgresql database, runs migrations, and then generates SQL Boiler ORM code.

SQL Boiler by default uses configuration files, this tool ignores all configuration files all options should
be passed as flags to this command.

Complete documentation of SQL Boiler is available at http://github.com/volatiletech/sqlboiler`,
		Example:       `sqlboiler-autogen`,
		PreRunE:       preRun,
		RunE:          run,
		PostRunE:      postRun,
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	cobra.OnInitialize(initConfig)

	// Set up the cobra root command flags
	rootCmd.PersistentFlags().StringVarP(&flagConfigFile, "config", "c", "", "Filename of config file to override default lookup")
	rootCmd.PersistentFlags().StringP("migrations", "m", "./migrations", "The name of the folder to read migrations from")
	rootCmd.PersistentFlags().StringP("output", "o", "models", "The name of the folder to output to")
	rootCmd.PersistentFlags().StringP("pkgname", "p", "models", "The name you wish to assign to your generated package")
	rootCmd.PersistentFlags().StringSliceP("templates", "", nil, "A templates directory, overrides the embedded template folders in sqlboiler")
	rootCmd.PersistentFlags().StringSliceP("tag", "t", nil, "Struct tags to be included on your models in addition to json, yaml, toml")
	rootCmd.PersistentFlags().StringSliceP("replace", "", nil, "Replace templates by directory: relpath/to_file.tpl:relpath/to_replacement.tpl")
	rootCmd.PersistentFlags().BoolP("debug", "d", false, "Debug mode prints stack traces on error")
	rootCmd.PersistentFlags().BoolP("no-context", "", false, "Disable context.Context usage in the generated code")
	rootCmd.PersistentFlags().BoolP("no-tests", "", false, "Disable generated go test files")
	rootCmd.PersistentFlags().BoolP("no-hooks", "", false, "Disable hooks feature for your models")
	rootCmd.PersistentFlags().BoolP("no-rows-affected", "", false, "Disable rows affected in the generated API")
	rootCmd.PersistentFlags().BoolP("no-auto-timestamps", "", false, "Disable automatic timestamps for created_at/updated_at")
	rootCmd.PersistentFlags().BoolP("no-driver-templates", "", false, "Disable parsing of templates defined by the database driver")
	rootCmd.PersistentFlags().BoolP("no-back-referencing", "", false, "Disable back referencing in the loaded relationship structs")
	rootCmd.PersistentFlags().BoolP("always-wrap-errors", "", false, "Wrap all returned errors with stacktraces, also sql.ErrNoRows")
	rootCmd.PersistentFlags().BoolP("add-global-variants", "", false, "Enable generation for global variants")
	rootCmd.PersistentFlags().BoolP("add-panic-variants", "", false, "Enable generation for panic variants")
	rootCmd.PersistentFlags().BoolP("add-soft-deletes", "", false, "Enable soft deletion by updating deleted_at timestamp")
	rootCmd.PersistentFlags().BoolP("add-enum-types", "", false, "Enable generation of types for enums")
	rootCmd.PersistentFlags().StringP("enum-null-prefix", "", "Null", "Name prefix of nullable enum types")
	rootCmd.PersistentFlags().BoolP("version", "", false, "Print the version")
	rootCmd.PersistentFlags().BoolP("wipe", "", false, "Delete the output folder (rm -rf) before generation to ensure sanity")
	rootCmd.PersistentFlags().StringP("struct-tag-casing", "", "snake", "Decides the casing for go structure tag names. camel, title or snake (default snake)")
	rootCmd.PersistentFlags().StringP("relation-tag", "r", "-", "Relationship struct tag name")
	rootCmd.PersistentFlags().StringSliceP("tag-ignore", "", nil, "List of column names that should have tags values set to '-' (ignored during parsing)")

	// hide flags not recommended for use
	rootCmd.PersistentFlags().MarkHidden("replace")

	viper.BindPFlags(rootCmd.PersistentFlags())
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	if err := rootCmd.Execute(); err != nil {
		if e, ok := err.(commandFailure); ok {
			fmt.Printf("Error: %v\n\n", string(e))
			rootCmd.Help()
		} else if !viper.GetBool("debug") {
			fmt.Printf("Error: %v\n", err)
		} else {
			fmt.Printf("Error: %+v\n", err)
		}

		os.Exit(1)
	}
}

type commandFailure string

func (c commandFailure) Error() string {
	return string(c)
}

func preRun(cmd *cobra.Command, args []string) error {
	var err error

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
	migrationsPath := viper.GetString("migrations")
	migrationsPath, err = filepath.Abs(migrationsPath)
	if err != nil {
		return errors.Wrap(err, "could not find absolute path to migrations")
	}

	// Create the local database in a temp directory.
	tempDir, err := os.MkdirTemp(".", ".sqlboiler-autogen-")
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

	cmdConfig = &boilingcore.Config{
		DriverName:        driverName,
		DriverConfig:      driverConfig,
		OutFolder:         viper.GetString("output"),
		PkgName:           viper.GetString("pkgname"),
		Debug:             viper.GetBool("debug"),
		AddGlobal:         viper.GetBool("add-global-variants"),
		AddPanic:          viper.GetBool("add-panic-variants"),
		AddSoftDeletes:    viper.GetBool("add-soft-deletes"),
		AddEnumTypes:      viper.GetBool("add-enum-types"),
		EnumNullPrefix:    viper.GetString("enum-null-prefix"),
		NoContext:         viper.GetBool("no-context"),
		NoTests:           viper.GetBool("no-tests"),
		NoHooks:           viper.GetBool("no-hooks"),
		NoRowsAffected:    viper.GetBool("no-rows-affected"),
		NoAutoTimestamps:  viper.GetBool("no-auto-timestamps"),
		NoDriverTemplates: viper.GetBool("no-driver-templates"),
		NoBackReferencing: viper.GetBool("no-back-referencing"),
		AlwaysWrapErrors:  viper.GetBool("always-wrap-errors"),
		Wipe:              viper.GetBool("wipe"),
		StructTagCasing:   strings.ToLower(viper.GetString("struct-tag-casing")), // camel | snake | title
		StructTagCases: boilingcore.StructTagCases{
			// make this compatible with the legacy struct-tag-casing config
			Json: withDefaultCase(viper.GetString("struct-tag-cases.json"), viper.GetString("struct-tag-casing")),
			Yaml: withDefaultCase(viper.GetString("struct-tag-cases.yaml"), viper.GetString("struct-tag-casing")),
			Toml: withDefaultCase(viper.GetString("struct-tag-cases.toml"), viper.GetString("struct-tag-casing")),
			Boil: withDefaultCase(viper.GetString("struct-tag-cases.boil"), viper.GetString("struct-tag-casing")),
		},
		TagIgnore:    viper.GetStringSlice("tag-ignore"),
		RelationTag:  viper.GetString("relation-tag"),
		TemplateDirs: viper.GetStringSlice("templates"),
		Tags:         viper.GetStringSlice("tag"),
		Replacements: viper.GetStringSlice("replace"),
		Aliases:      boilingcore.ConvertAliases(viper.Get("aliases")),
		TypeReplaces: boilingcore.ConvertTypeReplace(viper.Get("types")),
		AutoColumns: boilingcore.AutoColumns{
			Created: viper.GetString("auto-columns.created"),
			Updated: viper.GetString("auto-columns.updated"),
			Deleted: viper.GetString("auto-columns.deleted"),
		},
		Inflections: boilingcore.Inflections{
			Plural:        viper.GetStringMapString("inflections.plural"),
			PluralExact:   viper.GetStringMapString("inflections.plural_exact"),
			Singular:      viper.GetStringMapString("inflections.singular"),
			SingularExact: viper.GetStringMapString("inflections.singular_exact"),
			Irregular:     viper.GetStringMapString("inflections.irregular"),
		},
		ForeignKeys: boilingcore.ConvertForeignKeys(viper.Get("foreign_keys")),

		Version: sqlBoilerVersion,
	}

	if cmdConfig.Debug {
		fmt.Fprintln(os.Stderr, "using driver:", driverPath)
	}

	cmdConfig.Imports = configureImports()

	cmdState, err = boilingcore.New(cmdConfig)
	return err
}

func configureImports() importers.Collection {
	imports := importers.NewDefaultImports()

	mustMap := func(m importers.Map, err error) importers.Map {
		if err != nil {
			panic("failed to change viper interface into importers.Map: " + err.Error())
		}

		return m
	}

	if viper.IsSet("imports.all.standard") {
		imports.All.Standard = viper.GetStringSlice("imports.all.standard")
	}
	if viper.IsSet("imports.all.third_party") {
		imports.All.ThirdParty = viper.GetStringSlice("imports.all.third_party")
	}
	if viper.IsSet("imports.test.standard") {
		imports.Test.Standard = viper.GetStringSlice("imports.test.standard")
	}
	if viper.IsSet("imports.test.third_party") {
		imports.Test.ThirdParty = viper.GetStringSlice("imports.test.third_party")
	}
	if viper.IsSet("imports.singleton") {
		imports.Singleton = mustMap(importers.MapFromInterface(viper.Get("imports.singleton")))
	}
	if viper.IsSet("imports.test_singleton") {
		imports.TestSingleton = mustMap(importers.MapFromInterface(viper.Get("imports.test_singleton")))
	}
	if viper.IsSet("imports.based_on_type") {
		imports.BasedOnType = mustMap(importers.MapFromInterface(viper.Get("imports.based_on_type")))
	}

	return imports
}

func run(cmd *cobra.Command, args []string) error {
	return cmdState.Run()
}

func postRun(cmd *cobra.Command, args []string) error {
	return cmdState.Cleanup()
}

func withDefaultCase(configCase string, defaultCases ...string) boilingcore.TagCase {
	if len(configCase) > 0 {
		return boilingcore.TagCase(strings.ToLower(configCase))
	}

	for _, c := range defaultCases {
		if len(c) > 0 {
			return boilingcore.TagCase(strings.ToLower(c))
		}
	}

	return boilingcore.TagCaseSnake
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
			port, err := strconv.Atoi(strings.Trim(parts[1], "'"))
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
