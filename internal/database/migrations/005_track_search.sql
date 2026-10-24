ALTER TABLE tracks ADD COLUMN search_text TEXT NOT NULL DEFAULT '';
UPDATE tracks SET search_text=lower(title || ' ' || artist || ' ' || album);
