// CLAUDE:SUMMARY CRUD operations for the folders table — hierarchical content organization.
package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Folder is a logical grouping of extracted content.
type Folder struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	ParentID    string `json:"parent_id,omitempty"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
}

// InsertFolder creates a new folder.
func (s *Store) InsertFolder(ctx context.Context, f *Folder) error {
	now := time.Now().UnixMilli()
	if f.CreatedAt == 0 {
		f.CreatedAt = now
	}
	f.UpdatedAt = now

	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO folders (id, name, description, parent_id, created_at, updated_at)
		VALUES (?,?,?,?,?,?)`,
		f.ID, f.Name, f.Description, nullStr(f.ParentID), f.CreatedAt, f.UpdatedAt,
	)
	return err
}

// GetFolder retrieves a folder by ID.
func (s *Store) GetFolder(ctx context.Context, id string) (*Folder, error) {
	f := &Folder{}
	var parentID sql.NullString

	err := s.DB.QueryRowContext(ctx, `
		SELECT id, name, description, parent_id, created_at, updated_at
		FROM folders WHERE id = ?`, id).Scan(
		&f.ID, &f.Name, &f.Description, &parentID, &f.CreatedAt, &f.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	f.ParentID = parentID.String
	return f, nil
}

// ListFolders returns all folders.
func (s *Store) ListFolders(ctx context.Context) ([]*Folder, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, name, description, parent_id, created_at, updated_at
		FROM folders ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var folders []*Folder
	for rows.Next() {
		f := &Folder{}
		var parentID sql.NullString
		if err := rows.Scan(&f.ID, &f.Name, &f.Description, &parentID, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, err
		}
		f.ParentID = parentID.String
		folders = append(folders, f)
	}
	return folders, rows.Err()
}

// UpdateFolder updates a folder's name, description, and parent.
func (s *Store) UpdateFolder(ctx context.Context, f *Folder) error {
	f.UpdatedAt = time.Now().UnixMilli()
	_, err := s.DB.ExecContext(ctx, `
		UPDATE folders SET name=?, description=?, parent_id=?, updated_at=?
		WHERE id=?`,
		f.Name, f.Description, nullStr(f.ParentID), f.UpdatedAt, f.ID,
	)
	return err
}

// DeleteFolder removes a folder by ID.
func (s *Store) DeleteFolder(ctx context.Context, id string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM folders WHERE id = ?`, id)
	return err
}
