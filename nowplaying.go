package main

import (
	"time"

	"github.com/datahearth/streamline/extensions/sdk"
)

func init() {
	register("getNowPlaying", getNowPlaying)
}

// nowPlayingTTL bounds how long an entry outlives its track when no final
// scrobble removes it (a player closed mid-song), and is the whole lifetime of
// an entry whose track has no known duration.
const nowPlayingTTL = 10 * time.Minute

// maxPlayersPerUser bounds one account's entries: a client naming itself
// differently on every call would otherwise grow the table without end.
const maxPlayersPerUser = 8

// setNowPlaying records what the user's player is playing, dropping expired
// entries and, past maxPlayersPerUser, the user's oldest.
func setNowPlaying(userID uint32, player string, trackID, durationSec uint32) error {
	now := time.Now().UTC()
	until := now.Add(time.Duration(durationSec)*time.Second + nowPlayingTTL)
	return tx(func() error {
		if _, err := exec(
			"DELETE FROM now_playing WHERE expires_at < ?", formatStored(now),
		); err != nil {
			return err
		}
		if _, err := exec(
			`DELETE FROM now_playing WHERE user_id = ? AND player <> ? AND player NOT IN (
				SELECT player FROM now_playing WHERE user_id = ? AND player <> ?
				ORDER BY started_at DESC, player LIMIT ?)`,
			userID,
			player,
			userID,
			player,
			maxPlayersPerUser-1,
		); err != nil {
			return err
		}
		_, err := exec(
			`INSERT INTO now_playing (user_id, player, track_id, started_at, expires_at)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT (user_id, player) DO UPDATE SET track_id = excluded.track_id,
				started_at = excluded.started_at, expires_at = excluded.expires_at`,
			userID,
			player,
			trackID,
			formatStored(now),
			formatStored(until),
		)
		return err
	})
}

// finishNowPlaying drops the entry a final scrobble of the same track closes.
func finishNowPlaying(userID uint32, player string, trackID uint32) error {
	_, err := exec(
		"DELETE FROM now_playing WHERE user_id = ? AND player = ? AND track_id = ?",
		userID, player, trackID,
	)
	return err
}

func getNowPlaying(c *reqCtx) sdk.Response {
	now := time.Now().UTC()
	rows, err := query(
		`SELECT np.track_id, np.player, np.started_at, l.email
		FROM now_playing np JOIN streamline_user_logins l ON l.user_id = np.user_id
		WHERE np.expires_at >= ?
		ORDER BY np.started_at DESC, np.user_id, np.player`,
		formatStored(now),
	)
	if err != nil {
		return c.failErr(err)
	}
	trackIDs := make([]uint32, len(rows))
	for i, r := range rows {
		trackIDs[i] = r.uint(0)
	}
	songs, err := songsByID(trackIDs)
	if err != nil {
		return c.failErr(err)
	}
	out := &nowPlayingList{Entry: []nowPlayingEntry{}}
	for i, r := range rows {
		song, playable := songs[r.uint(0)]
		if !playable {
			continue
		}
		out.Entry = append(out.Entry, nowPlayingEntry{
			child:      song,
			Username:   r.str(3),
			MinutesAgo: int(now.Sub(r.time(2)).Minutes()),
			PlayerID:   i + 1,
			PlayerName: r.str(1),
		})
	}
	return c.respond(&body{NowPlaying: out})
}
