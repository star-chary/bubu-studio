package persistence

import (
	"context"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"
)

type LibraryImage struct {
	ID  string `json:"id"`
	URL string `json:"url"`
}

type LibraryPrompt struct {
	ID        string         `json:"id"`
	Title     string         `json:"title"`
	Content   string         `json:"content"`
	Images    []LibraryImage `json:"images"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
}

func (s *Store) LibraryPrompts(ctx context.Context, query string, limit, offset int) ([]LibraryPrompt, bool, error) {
	query = strings.TrimSpace(query)
	if utf8.RuneCountInString(query) > 100 || limit < 1 || limit > 100 || offset < 0 || offset > 100000 {
		return nil, false, ErrInvalid
	}
	rows, err := s.Pool.Query(ctx, `SELECT p.id::text,p.title,p.content,p.created_at,p.updated_at,
		COALESCE((SELECT json_agg(json_build_object('id',i.id::text,'url','/api/prompt-images/' || i.id::text) ORDER BY i.created_at,i.id)
		FROM prompt_library_images i WHERE i.prompt_id=p.id),'[]'::json)
		FROM prompt_library p WHERE ($1='' OR strpos(lower(p.title || ' ' || p.content),lower($1))>0)
		ORDER BY p.created_at DESC,p.id DESC LIMIT $2 OFFSET $3`, query, limit+1, offset)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	items := make([]LibraryPrompt, 0, limit)
	for rows.Next() {
		var p LibraryPrompt
		var images []byte
		if err := rows.Scan(&p.ID, &p.Title, &p.Content, &p.CreatedAt, &p.UpdatedAt, &images); err != nil {
			return nil, false, err
		}
		if err := json.Unmarshal(images, &p.Images); err != nil {
			return nil, false, err
		}
		items = append(items, p)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	return items, hasMore, nil
}

func validLibraryText(title, content string) bool {
	return utf8.RuneCountInString(title) >= 1 && utf8.RuneCountInString(title) <= 100 && utf8.RuneCountInString(content) >= 1 && utf8.RuneCountInString(content) <= 20000
}

func (s *Store) SaveLibraryPrompt(ctx context.Context, id, title, content string, create bool) error {
	title, content = strings.TrimSpace(title), strings.TrimSpace(content)
	if !ValidID(id) || !validLibraryText(title, content) {
		return ErrInvalid
	}
	if create {
		result, err := s.Pool.Exec(ctx, `INSERT INTO prompt_library(id,title,content) VALUES($1,$2,$3)
			ON CONFLICT(id) DO UPDATE SET title=EXCLUDED.title WHERE prompt_library.title=EXCLUDED.title AND prompt_library.content=EXCLUDED.content`, id, title, content)
		if err != nil {
			return mapError(err)
		}
		if result.RowsAffected() == 0 {
			return ErrConflict
		}
		return nil
	}
	result, err := s.Pool.Exec(ctx, `UPDATE prompt_library SET title=$2,content=$3,updated_at=now() WHERE id=$1`, id, title, content)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) LibraryPromptExists(ctx context.Context, id string) error {
	if !ValidID(id) {
		return ErrNotFound
	}
	var exists bool
	if err := s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM prompt_library WHERE id=$1)`, id).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	return nil
}

func (s *Store) AddLibraryImage(ctx context.Context, promptID, imageID, key string) error {
	if !ValidID(promptID) || !ValidID(imageID) {
		return ErrInvalid
	}
	_, err := s.Pool.Exec(ctx, `INSERT INTO prompt_library_images(id,prompt_id,object_key) VALUES($1,$2,$3)`, imageID, promptID, key)
	return mapError(err)
}

func (s *Store) LibraryImageExists(ctx context.Context, promptID, imageID string) (bool, error) {
	if !ValidID(promptID) || !ValidID(imageID) {
		return false, ErrInvalid
	}
	var exists bool
	err := s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM prompt_library_images WHERE prompt_id=$1 AND id=$2)`, promptID, imageID).Scan(&exists)
	return exists, err
}

func (s *Store) LibraryImageKey(ctx context.Context, id string) (string, error) {
	if !ValidID(id) {
		return "", ErrNotFound
	}
	var key string
	err := s.Pool.QueryRow(ctx, `SELECT object_key FROM prompt_library_images WHERE id=$1`, id).Scan(&key)
	return key, mapError(err)
}

func (s *Store) DeleteLibraryImage(ctx context.Context, promptID, imageID string) error {
	if !ValidID(promptID) || !ValidID(imageID) {
		return ErrNotFound
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	result, err := tx.Exec(ctx, `INSERT INTO prompt_image_cleanup(object_key) SELECT object_key FROM prompt_library_images WHERE prompt_id=$1 AND id=$2 ON CONFLICT DO NOTHING`, promptID, imageID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	if _, err = tx.Exec(ctx, `DELETE FROM prompt_library_images WHERE prompt_id=$1 AND id=$2`, promptID, imageID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) DeleteLibraryPrompt(ctx context.Context, promptID string) error {
	if !ValidID(promptID) {
		return ErrNotFound
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	// Lock the parent before taking the image snapshot, so concurrent uploads
	// cannot be cascaded away without entering the object cleanup queue.
	var locked string
	if err = tx.QueryRow(ctx, `SELECT id::text FROM prompt_library WHERE id=$1 FOR UPDATE`, promptID).Scan(&locked); err != nil {
		return mapError(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO prompt_image_cleanup(object_key) SELECT object_key FROM prompt_library_images WHERE prompt_id=$1 ON CONFLICT DO NOTHING`, promptID); err != nil {
		return err
	}
	result, err := tx.Exec(ctx, `DELETE FROM prompt_library WHERE id=$1`, promptID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return tx.Commit(ctx)
}

func (s *Store) QueuePromptImageCleanup(ctx context.Context, key string) error {
	_, err := s.Pool.Exec(ctx, `INSERT INTO prompt_image_cleanup(object_key) VALUES($1) ON CONFLICT DO NOTHING`, key)
	return err
}

func (s *Store) PendingPromptImageCleanup(ctx context.Context) (string, error) {
	var key string
	err := s.Pool.QueryRow(ctx, `SELECT object_key FROM prompt_image_cleanup ORDER BY created_at,object_key LIMIT 1`).Scan(&key)
	return key, mapError(err)
}

func (s *Store) FinishPromptImageCleanup(ctx context.Context, key string) error {
	_, err := s.Pool.Exec(ctx, `DELETE FROM prompt_image_cleanup WHERE object_key=$1`, key)
	return err
}
