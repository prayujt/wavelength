package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/prayujt/wavelength/internal/library"
	"github.com/prayujt/wavelength/internal/playfeed"
)

type PlayFeedStore struct{ DB *sql.DB }

func (s PlayFeedStore) Request(ctx context.Context, owner, root, id string) (playfeed.Receipt, error) {
	var r playfeed.Receipt
	var raw []byte
	err := s.DB.QueryRowContext(ctx, "SELECT request_hash,response_json FROM play_feed_requests WHERE owner=? AND root_id=? AND request_id=? AND created_at>?", owner, root, id, time.Now().Add(-24*time.Hour).Unix()).Scan(&r.Hash, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return r, playfeed.ErrNotFound
	}
	if err == nil {
		err = json.Unmarshal(raw, &r.Page)
	}
	return r, err
}
func (s PlayFeedStore) Continue(ctx context.Context, owner, root, cursor string) (playfeed.Continuation, error) {
	var c playfeed.Continuation
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return c, err
	}
	defer tx.Rollback()
	err = tx.QueryRowContext(ctx, `SELECT f.id FROM play_feeds f JOIN play_feed_pages p ON p.feed_id=f.id WHERE f.owner=? AND f.root_id=? AND p.next_cursor=? AND f.touched_at>?`, owner, root, cursor, time.Now().Add(-time.Hour).UnixMilli()).Scan(&c.FeedID)
	if errors.Is(err, sql.ErrNoRows) {
		return c, playfeed.ErrNotFound
	}
	if err != nil {
		return c, err
	}
	var raw []byte
	err = tx.QueryRowContext(ctx, "SELECT response_json FROM play_feed_pages WHERE feed_id=? AND cursor_in=?", c.FeedID, cursor).Scan(&raw)
	if err == nil {
		var p playfeed.Page
		if err = json.Unmarshal(raw, &p); err != nil {
			return c, err
		}
		c.Issued = &p
	} else if !errors.Is(err, sql.ErrNoRows) {
		return c, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT response_json FROM play_feed_pages WHERE feed_id=?", c.FeedID)
	if err != nil {
		return c, err
	}
	pages := 0
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return c, err
		}
		var p playfeed.Page
		if err = json.Unmarshal(raw, &p); err != nil {
			rows.Close()
			return c, err
		}
		for _, item := range p.Items {
			c.Seen = append(c.Seen, item.Track.ID)
		}
		pages++
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return c, err
	}
	if pages >= 100 && c.Issued == nil {
		return c, playfeed.ErrCapacity
	}
	return c, tx.Commit()
}
func (s PlayFeedStore) Save(ctx context.Context, owner, root string, in playfeed.Request, hash string, page playfeed.Page) (playfeed.Receipt, error) {
	receipt := playfeed.Receipt{Hash: hash, Page: page}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return receipt, err
	}
	defer tx.Rollback()
	now := time.Now()
	if _, err = tx.ExecContext(ctx, "DELETE FROM play_feed_requests WHERE created_at<=?", now.Add(-24*time.Hour).Unix()); err != nil {
		return receipt, err
	}
	var previousHash string
	var raw []byte
	err = tx.QueryRowContext(ctx, "SELECT request_hash,response_json FROM play_feed_requests WHERE owner=? AND root_id=? AND request_id=?", owner, root, in.RequestID).Scan(&previousHash, &raw)
	if err == nil {
		if previousHash != hash {
			return receipt, playfeed.ErrConflict
		}
		err = json.Unmarshal(raw, &receipt.Page)
		return receipt, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return receipt, err
	}
	for _, q := range []struct {
		sql    string
		cutoff int64
	}{
		{"DELETE FROM play_feeds WHERE touched_at<?", now.Add(-time.Hour).UnixMilli()},
		{"DELETE FROM play_feed_requests WHERE created_at<?", now.Add(-24 * time.Hour).Unix()},
		{"DELETE FROM play_feed_items WHERE created_at<?", now.Add(-30 * 24 * time.Hour).Unix()},
	} {
		if _, err = tx.ExecContext(ctx, q.sql, q.cutoff); err != nil {
			return receipt, err
		}
	}
	if in.Cursor == "" {
		_, err = tx.ExecContext(ctx, `DELETE FROM play_feeds WHERE id IN (SELECT id FROM play_feeds WHERE owner=? AND root_id=? ORDER BY touched_at DESC,id DESC LIMIT -1 OFFSET 127)`, owner, root)
		if err == nil {
			_, err = tx.ExecContext(ctx, "INSERT INTO play_feeds(id,owner,root_id,touched_at) VALUES(?,?,?,?)", page.FeedID, owner, root, now.UnixMilli())
		}
	} else {
		var feed string
		err = tx.QueryRowContext(ctx, `SELECT f.id FROM play_feeds f JOIN play_feed_pages p ON p.feed_id=f.id WHERE f.owner=? AND f.root_id=? AND p.next_cursor=?`, owner, root, in.Cursor).Scan(&feed)
		if errors.Is(err, sql.ErrNoRows) {
			return receipt, playfeed.ErrNotFound
		}
		if err != nil {
			return receipt, err
		}
		page.FeedID = feed
		err = tx.QueryRowContext(ctx, "SELECT response_json FROM play_feed_pages WHERE feed_id=? AND cursor_in=?", feed, in.Cursor).Scan(&raw)
		if err == nil {
			if err = json.Unmarshal(raw, &page); err != nil {
				return receipt, err
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return receipt, err
		}
		err = nil
	}
	if err != nil {
		return receipt, err
	}
	// Resolve current catalog records in the publication transaction, including a
	// concurrent deletion after ranking. A feed never publishes missing tracks.
	items := make([]playfeed.Item, 0, len(page.Items))
	for _, item := range page.Items {
		track, err := readTrack(tx.QueryRowContext(ctx, "SELECT "+trackColumns+" FROM tracks WHERE root_id=? AND id=?", root, item.Track.ID))
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return receipt, err
		}
		item.Track = track
		items = append(items, item)
		if _, err = tx.ExecContext(ctx, "INSERT INTO play_feed_items(id,owner,root_id,track_id,created_at) VALUES(?,?,?,?,?) ON CONFLICT(id) DO NOTHING", item.ID, owner, root, track.ID, now.Unix()); err != nil {
			return receipt, err
		}
	}
	page.Items = items
	raw, err = json.Marshal(page)
	if err != nil {
		return receipt, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO play_feed_pages(feed_id,cursor_in,next_cursor,response_json) VALUES(?,?,?,?) ON CONFLICT(feed_id,cursor_in) DO NOTHING", page.FeedID, in.Cursor, page.NextCursor, raw); err != nil {
		return receipt, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE play_feeds SET touched_at=? WHERE id=?", now.UnixMilli(), page.FeedID); err != nil {
		return receipt, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO play_feed_requests(owner,root_id,request_id,request_hash,response_json,created_at) VALUES(?,?,?,?,?,?)", owner, root, in.RequestID, hash, raw, now.Unix()); err != nil {
		return receipt, err
	}
	receipt.Page = page
	return receipt, tx.Commit()
}
func exclusionSQL(ids []string) (string, []any) {
	if len(ids) == 0 {
		return "", nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return " AND id NOT IN (" + strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",") + ")", args
}
func (s PlayFeedStore) Count(ctx context.Context, root string, excluded []string) (int, error) {
	clause, args := exclusionSQL(excluded)
	args = append([]any{root}, args...)
	var n int
	err := s.DB.QueryRowContext(ctx, "SELECT count(*) FROM tracks WHERE root_id=? AND content_type LIKE 'audio/%'"+clause, args...).Scan(&n)
	return n, err
}
func (s PlayFeedStore) Candidates(ctx context.Context, root string, artists, excluded []string, limit int) ([]library.Track, error) {
	if limit < 1 || limit > playfeed.MaxCandidates {
		return nil, playfeed.ErrInvalid
	}
	out := []library.Track{}
	query := func(favorite bool, n int) error {
		exclude := append([]string{}, excluded...)
		for _, t := range out {
			exclude = append(exclude, t.ID)
		}
		clause, args := exclusionSQL(exclude)
		args = append([]any{root}, args...)
		q := "SELECT " + trackColumns + " FROM tracks WHERE root_id=? AND content_type LIKE 'audio/%'" + clause
		if favorite {
			q += " AND artist IN (" + strings.TrimSuffix(strings.Repeat("?,", len(artists)), ",") + ")"
			for _, artist := range artists {
				args = append(args, artist)
			}
		}
		q += " ORDER BY random() LIMIT ?"
		args = append(args, n)
		rows, err := s.DB.QueryContext(ctx, q, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			track, err := readTrack(rows)
			if err != nil {
				return err
			}
			out = append(out, track)
		}
		return rows.Err()
	}
	if len(artists) > 0 {
		if err := query(true, limit*2/3); err != nil {
			return nil, err
		}
	}
	if err := query(false, limit-len(out)); err != nil {
		return nil, err
	}
	return out, nil
}

var _ playfeed.Store = PlayFeedStore{}
