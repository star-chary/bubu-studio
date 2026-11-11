-- Prompt library content is separate from private canvas assets.
CREATE TABLE prompt_library (
    id uuid PRIMARY KEY,
    title text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 100),
    content text NOT NULL CHECK (char_length(content) BETWEEN 1 AND 20000),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX prompt_library_recent ON prompt_library(created_at DESC, id DESC);

CREATE TABLE prompt_library_images (
    id uuid PRIMARY KEY,
    prompt_id uuid NOT NULL REFERENCES prompt_library(id) ON DELETE CASCADE,
    object_key text NOT NULL UNIQUE,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX prompt_library_images_order ON prompt_library_images(prompt_id, created_at, id);

-- Retain failed object deletions for retry on a subsequent admin operation.
CREATE TABLE prompt_image_cleanup (
    object_key text PRIMARY KEY,
    created_at timestamptz NOT NULL DEFAULT now()
);
