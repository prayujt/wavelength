ALTER TABLE tracks DROP COLUMN scan_id;
ALTER TABLE library_roots ADD COLUMN revision INTEGER NOT NULL DEFAULT 0;
UPDATE library_roots SET revision = CAST((julianday('now') - 2440587.5) * 86400000 AS INTEGER);

-- Client cache invalidation follows database changes, never filesystem state.
CREATE TRIGGER tracks_insert_revision AFTER INSERT ON tracks BEGIN
    UPDATE library_roots SET revision = max(revision + 1, CAST((julianday('now') - 2440587.5) * 86400000 AS INTEGER)) WHERE id = NEW.root_id;
END;
CREATE TRIGGER tracks_update_revision AFTER UPDATE ON tracks BEGIN
    UPDATE library_roots SET revision = max(revision + 1, CAST((julianday('now') - 2440587.5) * 86400000 AS INTEGER)) WHERE id IN (OLD.root_id, NEW.root_id);
END;
CREATE TRIGGER tracks_delete_revision AFTER DELETE ON tracks BEGIN
    UPDATE library_roots SET revision = max(revision + 1, CAST((julianday('now') - 2440587.5) * 86400000 AS INTEGER)) WHERE id = OLD.root_id;
END;
