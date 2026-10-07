ALTER TABLE canvases ADD COLUMN title text NOT NULL DEFAULT '未命名画布';
ALTER TABLE canvases ADD CONSTRAINT canvas_title_length CHECK (char_length(btrim(title)) BETWEEN 1 AND 80);
CREATE INDEX canvases_recent_idx ON canvases(updated_at DESC, id DESC);
