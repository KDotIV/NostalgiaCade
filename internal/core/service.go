package core

import (
	"context"
	"database/sql"
	"github.com/KdotIV/NostalgiaCade/internal/library"
	"runtime"
	"time"
)

const ProtocolVersion = 1
const Version = "0.4.0-dev"

type Genre struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	TargetEntries int    `json:"targetEntries"`
}

type Status struct {
	Version         string `json:"version"`
	ProtocolVersion int    `json:"protocolVersion"`
	Platform        string `json:"platform"`
	DatabaseVersion string `json:"databaseVersion"`
	LocalGameCount  int    `json:"localGameCount"`
	Readiness       string `json:"readiness"`
	UptimeSeconds   int64  `json:"uptimeSeconds"`
	SetupRequired   bool   `json:"setupRequired"`
}

type Service struct {
	DB              *sql.DB
	DatabaseVersion string
	StartedAt       time.Time
	Library         *library.Manager
}

func (s *Service) Status(ctx context.Context) (Status, error) {
	result := Status{Version: Version, ProtocolVersion: ProtocolVersion, Platform: runtime.GOOS + "/" + runtime.GOARCH,
		DatabaseVersion: s.DatabaseVersion, UptimeSeconds: int64(time.Since(s.StartedAt).Seconds())}
	if err := s.DB.QueryRowContext(ctx, "SELECT count(*) FROM local_games").Scan(&result.LocalGameCount); err != nil {
		return result, err
	}
	err := s.DB.QueryRowContext(ctx, "SELECT readiness FROM active_suite WHERE singleton = 1").Scan(&result.Readiness)
	if err == nil && s.Library != nil {
		setup, e := s.Library.SetupStatus(ctx)
		err = e
		result.SetupRequired = !setup.Configured
	}
	return result, err
}

func (s *Service) Genres(ctx context.Context) ([]Genre, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT id, name, target_entries FROM genres ORDER BY display_order")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	genres := []Genre{}
	for rows.Next() {
		var genre Genre
		if err := rows.Scan(&genre.ID, &genre.Name, &genre.TargetEntries); err != nil {
			return nil, err
		}
		genres = append(genres, genre)
	}
	return genres, rows.Err()
}
