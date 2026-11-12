package main

import "strconv"

// schemaVersion names the marker table ensureSchema looks for. Bump it when
// the statements below change; every statement is idempotent, so a request
// that finds no marker for this version runs them all again.
const schemaVersion = 1

// The extension's own tables. The host refuses CREATE INDEX (SQLite asks the
// authorizer for a REINDEX it does not allow), so a secondary index is a
// UNIQUE constraint that is trivially satisfied, such as (owner_id, id).
//
// A user id is Streamline's (streamline_users.id),
// a track, album or artist id is the row id of the core view, and times are
// stored by storedTime. The host does not enforce foreign keys across the
// attached core database and these tables declare none: a child row is
// deleted with its parent by the handler that deletes the parent, in one
// transaction.
var schema = []string{
	// One row per account that has set a password or been given settings; an
	// account with no row has every permission on and no password. password is
	// readable because the token scheme needs it, which is why it is never the
	// Streamline login password.
	`CREATE TABLE IF NOT EXISTS accounts(
		user_id INTEGER PRIMARY KEY,
		password TEXT NOT NULL DEFAULT '',
		password_created_at TEXT,
		last_used_at TEXT,
		last_client TEXT NOT NULL DEFAULT '',
		can_stream INTEGER NOT NULL DEFAULT 1,
		can_download INTEGER NOT NULL DEFAULT 1,
		can_manage_playlists INTEGER NOT NULL DEFAULT 1,
		can_share INTEGER NOT NULL DEFAULT 1,
		can_rate INTEGER NOT NULL DEFAULT 1,
		can_change_password INTEGER NOT NULL DEFAULT 1,
		max_bit_rate INTEGER NOT NULL DEFAULT 0 CHECK (max_bit_rate BETWEEN 0 AND 320)
	)`,

	// One listener's star, rating (0 is unrated) and play count on an artist,
	// album or track. target_kind is an ids.Kind* of ar, al or tr.
	`CREATE TABLE IF NOT EXISTS annotations(
		user_id INTEGER NOT NULL,
		target_kind TEXT NOT NULL,
		target_id INTEGER NOT NULL,
		starred_at TEXT,
		rating INTEGER NOT NULL DEFAULT 0 CHECK (rating BETWEEN 0 AND 5),
		play_count INTEGER NOT NULL DEFAULT 0,
		played_at TEXT,
		PRIMARY KEY (user_id, target_kind, target_id),
		UNIQUE (target_kind, target_id, user_id)
	) WITHOUT ROWID`,

	`CREATE TABLE IF NOT EXISTS playlists(
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		owner_id INTEGER NOT NULL,
		name TEXT NOT NULL,
		comment TEXT NOT NULL DEFAULT '',
		public INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL,
		changed_at TEXT NOT NULL,
		UNIQUE (owner_id, id)
	)`,
	// A playlist's entries in order, a track twice allowed; a track whose
	// file is gone is skipped on read, not chased on delete.
	`CREATE TABLE IF NOT EXISTS playlist_tracks(
		playlist_id INTEGER NOT NULL,
		position INTEGER NOT NULL,
		track_id INTEGER NOT NULL,
		PRIMARY KEY (playlist_id, position)
	) WITHOUT ROWID`,

	// The queue a client saves for another device to pick up: one per user,
	// overwritten whole by every save. current_index is an index into the
	// entries, not a track id, since the queue may hold a track twice.
	`CREATE TABLE IF NOT EXISTS play_queues(
		user_id INTEGER PRIMARY KEY,
		current_index INTEGER NOT NULL DEFAULT 0,
		position_ms INTEGER NOT NULL DEFAULT 0 CHECK (position_ms >= 0),
		changed_by TEXT NOT NULL DEFAULT '',
		changed_at TEXT NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS play_queue_entries(
		user_id INTEGER NOT NULL,
		position INTEGER NOT NULL,
		track_id INTEGER NOT NULL,
		PRIMARY KEY (user_id, position)
	) WITHOUT ROWID`,

	`CREATE TABLE IF NOT EXISTS bookmarks(
		user_id INTEGER NOT NULL,
		track_id INTEGER NOT NULL,
		position_ms INTEGER NOT NULL CHECK (position_ms >= 0),
		comment TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL,
		changed_at TEXT NOT NULL,
		PRIMARY KEY (user_id, track_id)
	) WITHOUT ROWID`,

	// A public link served at /share/{token}. The token is the only credential
	// a visitor holds.
	`CREATE TABLE IF NOT EXISTS shares(
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		token TEXT NOT NULL UNIQUE,
		owner_id INTEGER NOT NULL,
		description TEXT NOT NULL DEFAULT '',
		expires_at TEXT,
		last_visited_at TEXT,
		visit_count INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL,
		UNIQUE (owner_id, id)
	)`,
	// The Subsonic ids the share was created from (tracks, albums, a
	// playlist, an artist), expanded when the link is opened.
	`CREATE TABLE IF NOT EXISTS share_entries(
		share_id INTEGER NOT NULL,
		position INTEGER NOT NULL,
		item TEXT NOT NULL,
		PRIMARY KEY (share_id, position)
	) WITHOUT ROWID`,

	`CREATE TABLE IF NOT EXISTS radio_stations(
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		stream_url TEXT NOT NULL,
		homepage_url TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL
	)`,

	// What scrobble submission=false reports: one row per user and client,
	// dropped once expires_at passes.
	`CREATE TABLE IF NOT EXISTS now_playing(
		user_id INTEGER NOT NULL,
		player TEXT NOT NULL,
		track_id INTEGER NOT NULL,
		started_at TEXT NOT NULL,
		expires_at TEXT NOT NULL,
		PRIMARY KEY (user_id, player)
	) WITHOUT ROWID`,
}

// ensureSchema creates the tables when this database has not been prepared
// by the current version. The host's authorizer refuses PRAGMA, so the
// version is the name of an empty marker table created last: once it exists
// the check costs one read of sqlite_master. Every request calls it because a
// guest instance keeps nothing between requests.
func ensureSchema() error {
	marker := "schema_v" + strconv.Itoa(schemaVersion)
	_, found, err := queryRow(
		"SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = ?", marker,
	)
	if err != nil || found {
		return err
	}
	return tx(func() error {
		for _, stmt := range schema {
			if _, err := exec(stmt); err != nil {
				return err
			}
		}
		_, err := exec("CREATE TABLE IF NOT EXISTS " + marker + "(done INTEGER)")
		return err
	})
}
