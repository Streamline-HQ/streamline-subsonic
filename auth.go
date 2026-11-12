package main

import (
	"strings"
	"time"

	"github.com/datahearth/streamline/extensions/sdk"
	"github.com/streamline-hq/streamline-subsonic/creds"
	"github.com/streamline-hq/streamline-subsonic/envelope"
)

// permission is a Subsonic role an admin switches per account. Each gates
// Subsonic calls only, never anything in Streamline's web UI.
type permission string

const (
	permStream         permission = "streamRole"
	permDownload       permission = "downloadRole"
	permPlaylists      permission = "playlistRole"
	permShare          permission = "shareRole"
	permRate           permission = "commentRole"
	permChangePassword permission = "settingsRole"
)

// account is the signed-in Subsonic user: who Streamline says they are and
// what this extension lets them do.
type account struct {
	ID          uint32
	Email       string
	Role        string
	DisplayName string

	CanStream          bool
	CanDownload        bool
	CanManagePlaylists bool
	CanShare           bool
	CanRate            bool
	CanChangePassword  bool
	// MaxBitRate is the kbps ceiling on every stream; 0 is none.
	MaxBitRate uint16
}

func (a *account) allowed(p permission) bool {
	switch p {
	case permStream:
		return a.CanStream
	case permDownload:
		return a.CanDownload
	case permPlaylists:
		return a.CanManagePlaylists
	case permShare:
		return a.CanShare
	case permRate:
		return a.CanRate
	case permChangePassword:
		return a.CanChangePassword
	}
	return false
}

func (a *account) isAdmin() bool { return a.Role == "admin" }

// touchInterval is how often a successful sign-in is recorded, so a stream of
// requests costs one write in five minutes.
const touchInterval = 5 * time.Minute

const accountColumns = `COALESCE(a.can_stream, 1), COALESCE(a.can_download, 1),
	COALESCE(a.can_manage_playlists, 1), COALESCE(a.can_share, 1),
	COALESCE(a.can_rate, 1), COALESCE(a.can_change_password, 1),
	COALESCE(a.max_bit_rate, 0)`

// scanAccount reads l.user_id, l.email, u.role, u.display_name then
// accountColumns.
func scanAccount(r row) *account {
	return &account{
		ID:                 r.uint(0),
		Email:              r.str(1),
		Role:               r.str(2),
		DisplayName:        r.str(3),
		CanStream:          r.bool(4),
		CanDownload:        r.bool(5),
		CanManagePlaylists: r.bool(6),
		CanShare:           r.bool(7),
		CanRate:            r.bool(8),
		CanChangePassword:  r.bool(9),
		MaxBitRate:         uint16(min(max(r.int(10), 0), maxBitRateCeiling)),
	}
}

// loadAccount reads an account by user id, whether or not it has a password.
// found is false for an id Streamline does not have.
func loadAccount(userID uint32) (acc *account, found bool, err error) {
	r, found, err := queryRow(
		`SELECT l.user_id, l.email, u.role, u.display_name, `+accountColumns+`
		FROM streamline_user_logins l
		JOIN streamline_users u ON u.id = l.user_id
		LEFT JOIN accounts a ON a.user_id = l.user_id
		WHERE l.user_id = ?`,
		userID,
	)
	if err != nil || !found {
		return nil, false, err
	}
	return scanAccount(r), true, nil
}

// loadAccountByEmail reads an account by the login the Subsonic u parameter
// carries; the user name is the email, matched without regard to case.
func loadAccountByEmail(email string) (acc *account, found bool, err error) {
	r, found, err := queryRow(
		`SELECT l.user_id, l.email, u.role, u.display_name, `+accountColumns+`
		FROM streamline_user_logins l
		JOIN streamline_users u ON u.id = l.user_id
		LEFT JOIN accounts a ON a.user_id = l.user_id
		WHERE l.email = ?`,
		strings.ToLower(strings.TrimSpace(email)),
	)
	if err != nil || !found {
		return nil, false, err
	}
	return scanAccount(r), true, nil
}

// authenticate resolves the caller from the u parameter and checks the
// credentials. An unknown user, no Subsonic password and a wrong password all
// answer error 40 with one message, so the response does not reveal which
// logins exist.
func (c *reqCtx) authenticate() error {
	login := c.param("u")
	if login == "" {
		return envelope.MissingParam("u")
	}
	acc, found, err := loadAccountByEmail(login)
	if err != nil {
		return err
	}
	if !found {
		return envelope.WrongCredentials()
	}
	stored, err := storedPassword(acc.ID)
	if err != nil {
		return err
	}
	if !creds.Check(stored, c.params) {
		return envelope.WrongCredentials()
	}
	c.user = acc
	touchAccount(acc.ID, c.param("c"))
	return nil
}

func storedPassword(userID uint32) (string, error) {
	r, found, err := queryRow(
		"SELECT password FROM accounts WHERE user_id = ?", userID,
	)
	if err != nil || !found {
		return "", err
	}
	return r.str(0), nil
}

// touchAccount records a successful sign-in and the client that made it. It
// is bookkeeping: a failure is logged and never fails the request.
func touchAccount(userID uint32, client string) {
	now := time.Now().UTC()
	_, err := exec(
		`UPDATE accounts SET last_used_at = ?, last_client = ?
		WHERE user_id = ? AND (last_used_at IS NULL OR last_used_at < ?)`,
		formatStored(now),
		creds.SanitizeClient(client),
		userID,
		formatStored(now.Add(-touchInterval)),
	)
	if err != nil {
		sdk.Log("warn", "recording a sign-in failed", "error", err.Error())
	}
}

// storePassword makes password the account's Subsonic password and restarts
// its "last used" record. It is the one writer of the column, for the
// credential card and for changePassword alike.
func storePassword(userID uint32, password string) error {
	_, err := exec(
		`INSERT INTO accounts(user_id, password, password_created_at)
		VALUES(?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET
			password = excluded.password,
			password_created_at = excluded.password_created_at,
			last_used_at = NULL,
			last_client = ''`,
		userID, password, nowStored(),
	)
	return err
}
