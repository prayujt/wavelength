CREATE TABLE play_feeds (
 id TEXT PRIMARY KEY,
 owner TEXT NOT NULL,
 root_id TEXT NOT NULL REFERENCES library_roots(id),
 touched_at INTEGER NOT NULL
);
CREATE INDEX play_feeds_owner ON play_feeds(owner,root_id,touched_at);
CREATE TABLE play_feed_pages (
 feed_id TEXT NOT NULL REFERENCES play_feeds(id) ON DELETE CASCADE,
 cursor_in TEXT NOT NULL,
 next_cursor TEXT NOT NULL,
 response_json BLOB NOT NULL,
 PRIMARY KEY(feed_id,cursor_in)
);
CREATE INDEX play_feed_next ON play_feed_pages(next_cursor) WHERE next_cursor<>'';
CREATE TABLE play_feed_requests (
 owner TEXT NOT NULL,
 root_id TEXT NOT NULL REFERENCES library_roots(id),
 request_id TEXT NOT NULL,
 request_hash TEXT NOT NULL,
 response_json BLOB NOT NULL,
 created_at INTEGER NOT NULL,
 PRIMARY KEY(owner,root_id,request_id)
);
CREATE INDEX play_feed_requests_age ON play_feed_requests(created_at);
-- Attribution outlives feed cursors, including delayed offline listening reports.
CREATE TABLE play_feed_items (
 id TEXT PRIMARY KEY,
 owner TEXT NOT NULL,
 root_id TEXT NOT NULL REFERENCES library_roots(id),
 track_id TEXT NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
 created_at INTEGER NOT NULL
);
CREATE INDEX play_feed_items_age ON play_feed_items(created_at);
