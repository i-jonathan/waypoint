package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"waypoint/internal/config"
	"waypoint/internal/domain"
)

func TestCreateMakesParentDirsThenOpenLoads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a", "b", "board.md")
	ref := config.ProjectRef{Name: "N", Storage: "markdown", Path: path}

	repo, err := Create(ref, domain.NewBoard("N", "T").Settings)
	if err != nil {
		t.Fatal(err)
	}
	if repo.Path() != path {
		t.Fatalf("path = %q", repo.Path())
	}

	opened, err := Open(ref)
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := opened.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if b.Settings.Name != "N" {
		t.Fatalf("name = %q", b.Settings.Name)
	}
}

func TestCreateRefusesExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "board.md")
	if err := os.WriteFile(path, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Create(config.ProjectRef{Storage: "markdown", Path: path}, domain.NewBoard("N", "T").Settings)
	if !errors.Is(err, os.ErrExist) {
		t.Fatalf("got %v, want os.ErrExist", err)
	}
	if data, _ := os.ReadFile(path); string(data) != "keep" {
		t.Fatalf("file was changed: %q", data)
	}
}

func TestUnknownStorage(t *testing.T) {
	_, err := Open(config.ProjectRef{Name: "X", Storage: "carrier-pigeon", Path: "/x"})
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("got %v", err)
	}
}

func TestSniff(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "a.db")
	if _, err := Create(config.ProjectRef{Name: "N", Storage: "sqlite", Path: db}, domain.NewBoard("N", "T").Settings); err != nil {
		t.Fatal(err)
	}
	md := filepath.Join(dir, "a.md")
	if _, err := Create(config.ProjectRef{Name: "N", Storage: "markdown", Path: md}, domain.NewBoard("N", "T").Settings); err != nil {
		t.Fatal(err)
	}
	empty := filepath.Join(dir, "empty")
	os.WriteFile(empty, nil, 0o644)

	for path, want := range map[string]string{db: "sqlite", md: "markdown"} {
		if got, err := Sniff(path); err != nil || got != want {
			t.Errorf("Sniff(%s) = %q, %v; want %q", filepath.Base(path), got, err, want)
		}
	}
	if _, err := Sniff(empty); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("Sniff(empty) = %v, want domain.ErrInvalid", err)
	}
	if _, err := Sniff(filepath.Join(dir, "missing")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Sniff(missing) = %v, want os.ErrNotExist", err)
	}
}

func TestCreateThenOpenSQLite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a", "b", "board.db")
	ref := config.ProjectRef{Name: "N", Storage: "sqlite", Path: path}
	if _, err := Create(ref, domain.NewBoard("N", "T").Settings); err != nil {
		t.Fatal(err)
	}
	repo, err := Open(ref)
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := repo.Load(context.Background())
	if err != nil || b.Settings.Name != "N" {
		t.Fatalf("Load = %+v, %v", b, err)
	}
	if _, err := Create(ref, domain.NewBoard("N", "T").Settings); !errors.Is(err, os.ErrExist) {
		t.Fatalf("second Create = %v, want os.ErrExist", err)
	}
}
