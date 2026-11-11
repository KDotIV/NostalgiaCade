// ncade-catalog is an admin-side tool, never part of the cabinet bundle.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/KdotIV/NostalgiaCade/internal/database"
	"github.com/KdotIV/NostalgiaCade/internal/emucatalog"
	"os"
	"time"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	action := flag.String("action", "inspect", "inspect, publish, or export")
	file := flag.String("file", "", "Reviewed catalog JSON; omitted uses bundled snapshot")
	subject := flag.String("subject", "", "Publisher attribution; database role supplies authorization")
	flag.Parse()
	if *action != "inspect" && *action != "publish" && *action != "export" {
		return fmt.Errorf("unknown action")
	}
	c := emucatalog.Builtin()
	if *action == "publish" {
		if *file != "" {
			f, err := os.Open(*file)
			if err != nil {
				return err
			}
			defer f.Close()
			c, err = emucatalog.Read(f)
			if err != nil {
				return err
			}
		}
		if err := c.Validate(); err != nil {
			return err
		}
		if *subject == "" {
			return fmt.Errorf("--subject is required to publish")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := database.OpenPostgres(ctx, os.Getenv("NCADE_POSTGRES_DSN"))
	if err != nil {
		return err
	}
	defer db.Close()
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if *action == "inspect" {
		var databaseName, version string
		if err = db.QueryRowContext(ctx, `SELECT current_database()`).Scan(&databaseName); err != nil {
			return fmt.Errorf("cannot inspect database")
		}
		var history sql.NullString
		if err = db.QueryRowContext(ctx, `SELECT to_regclass('public.schema_migrations')::text`).Scan(&history); err != nil {
			return fmt.Errorf("cannot inspect migration history")
		}
		if history.Valid {
			if err = db.QueryRowContext(ctx, `SELECT COALESCE(max(version),'') FROM public.schema_migrations`).Scan(&version); err != nil {
				return fmt.Errorf("migration history has an unexpected format")
			}
		} else {
			var cleanup bool
			if err = db.QueryRowContext(ctx, `SELECT to_regclass('catalog.cleanup_migrations') IS NOT NULL`).Scan(&cleanup); err != nil {
				return fmt.Errorf("cannot inspect cleanup history")
			}
			if cleanup {
				if err = db.QueryRowContext(ctx, `SELECT COALESCE(max(version),'') FROM catalog.cleanup_migrations`).Scan(&version); err != nil {
					return fmt.Errorf("cannot read cleanup history")
				}
			}
		}
		rows, err := db.QueryContext(ctx, `SELECT table_schema||'.'||table_name,column_name,data_type FROM information_schema.columns WHERE table_schema IN ('public','catalog','management') ORDER BY table_schema,table_name,ordinal_position`)
		if err != nil {
			return fmt.Errorf("cannot inspect schema")
		}
		defer rows.Close()
		tables := map[string][]string{}
		for rows.Next() {
			var table, column, kind string
			if err = rows.Scan(&table, &column, &kind); err != nil {
				return fmt.Errorf("cannot inspect schema")
			}
			tables[table] = append(tables[table], column+":"+kind)
		}
		if rows.Err() != nil {
			return fmt.Errorf("cannot inspect schema")
		}
		return encoder.Encode(map[string]any{"database": databaseName, "schemaVersion": version, "tables": tables})
	}
	if *action == "publish" {
		if err = emucatalog.Publish(ctx, db, c, *subject); err != nil {
			return fmt.Errorf("catalog publication failed; check schema, database privileges and release conflicts")
		}
		return encoder.Encode(map[string]any{"published": len(c.Packages)})
	}
	c, err = emucatalog.Export(ctx, db)
	if err != nil {
		return fmt.Errorf("catalog export failed; check schema and read privileges")
	}
	return encoder.Encode(c)
}
