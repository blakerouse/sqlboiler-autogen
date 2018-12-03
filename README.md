# SQLBoiler Autogen
Tool to automatically run migrations on a locally created instance of a database then use sqlboiler to generate the go code.

## About
Easily generate the model code from your database schema directly from the migrations. The removes the need to manually create
a database, run the migrations, then generate the model code with SQLBoiler. Using `//go:generate sqlboiler-autogen` works
directly with just a `migrations` folder.

## Requirements
This currently only supports PostgreSQL and golang-migrate/migrate. Read the documentation at https://github.com/golang-migrate/migrate
on how to setup the migrations. This tool also uses the new Go modules feature so golang 1.11+ is required.

## Installation
Simply go get the tool, with 1.11+ it will pull in all the other requirements.

`go get github.com/blakerouse/sqlboiler-autogen`

## Usage
Add the generate statement to your code.

`//go:generate sqlboiler-autogen`

Use `sqlboiler-autogen --help` for information on the available options.
