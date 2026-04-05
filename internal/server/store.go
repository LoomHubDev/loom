package server

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/constructspace/loom/internal/storage"
)

// projectDB holds a database and object store for a single project.
type projectDB struct {
	db    *sql.DB
	store *storage.ObjectStore
}

// ProjectStore manages per-project SQLite databases and object stores.
type ProjectStore struct {
	dataDir string
	dbs     map[string]*projectDB // key: "owner/name"
	mu      sync.RWMutex
}

// NewProjectStore creates a ProjectStore rooted at dataDir.
func NewProjectStore(dataDir string) *ProjectStore {
	return &ProjectStore{
		dataDir: dataDir,
		dbs:     make(map[string]*projectDB),
	}
}

// GetOrCreate opens or creates a project database for the given owner/name.
func (ps *ProjectStore) GetOrCreate(owner, name string) (*projectDB, error) {
	key := owner + "/" + name

	ps.mu.RLock()
	if pdb, ok := ps.dbs[key]; ok {
		ps.mu.RUnlock()
		return pdb, nil
	}
	ps.mu.RUnlock()

	ps.mu.Lock()
	defer ps.mu.Unlock()

	// Double-check after acquiring write lock.
	if pdb, ok := ps.dbs[key]; ok {
		return pdb, nil
	}

	projDir := filepath.Join(ps.dataDir, "projects", owner, name)
	if err := os.MkdirAll(projDir, 0755); err != nil {
		return nil, fmt.Errorf("create project dir: %w", err)
	}

	dbPath := filepath.Join(projDir, "loom.db")
	db, err := storage.InitDB(dbPath)
	if err != nil {
		return nil, fmt.Errorf("init project db: %w", err)
	}

	objDir := filepath.Join(projDir, "objects")
	store, err := storage.NewObjectStore(objDir, db)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("init object store: %w", err)
	}

	pdb := &projectDB{db: db, store: store}
	ps.dbs[key] = pdb
	return pdb, nil
}

// Get opens an existing project database. Returns an error if it doesn't exist.
func (ps *ProjectStore) Get(owner, name string) (*projectDB, error) {
	key := owner + "/" + name

	ps.mu.RLock()
	if pdb, ok := ps.dbs[key]; ok {
		ps.mu.RUnlock()
		return pdb, nil
	}
	ps.mu.RUnlock()

	ps.mu.Lock()
	defer ps.mu.Unlock()

	if pdb, ok := ps.dbs[key]; ok {
		return pdb, nil
	}

	projDir := filepath.Join(ps.dataDir, "projects", owner, name)
	dbPath := filepath.Join(projDir, "loom.db")

	db, err := storage.OpenDB(dbPath)
	if err != nil {
		return nil, fmt.Errorf("open project db: %w", err)
	}

	objDir := filepath.Join(projDir, "objects")
	store, err := storage.NewObjectStore(objDir, db)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("open object store: %w", err)
	}

	pdb := &projectDB{db: db, store: store}
	ps.dbs[key] = pdb
	return pdb, nil
}

// Close closes all open project databases.
func (ps *ProjectStore) Close() {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	for _, pdb := range ps.dbs {
		pdb.db.Close()
	}
	ps.dbs = make(map[string]*projectDB)
}
