package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
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

func TestSQLiteIsNotBuiltYet(t *testing.T) {
	ref := config.ProjectRef{Name: "S", Storage: "sqlite", Path: filepath.Join(t.TempDir(), "s.db")}
	if _, err := Open(ref); err == nil || !strings.Contains(err.Error(), "sqlite storage arrives in a later version") {
		t.Fatalf("Open: %v", err)
	}
	if _, err := Create(ref, domain.NewBoard("S", "T").Settings); err == nil || !strings.Contains(err.Error(), "sqlite storage arrives in a later version") {
		t.Fatalf("Create: %v", err)
	}
	if _, err := os.Stat(ref.Path); err == nil {
		t.Fatal("a file was created")
	}
}

func TestUnknownStorage(t *testing.T) {
	_, err := Open(config.ProjectRef{Name: "X", Storage: "carrier-pigeon", Path: "/x"})
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("got %v", err)
	}
}
