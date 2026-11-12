package main

import (
	"encoding/hex"
	"strings"

	"github.com/datahearth/streamline/extensions/sdk"
	"github.com/streamline-hq/streamline-subsonic/envelope"
)

// minPasswordLen is the shortest Subsonic password changePassword accepts.
const minPasswordLen = 8

func init() {
	register("getUser", getUser)
	register("getUsers", getUsers)
	register("createUser", usersManagedInStreamline)
	register("updateUser", usersManagedInStreamline)
	register("deleteUser", usersManagedInStreamline)
	register("changePassword", changePassword)
}

// usersSubsonicUser renders an account. Jukebox, podcasts, uploads, cover art
// edits and video conversion do not exist here, so their roles are false.
func usersSubsonicUser(a *account) subsonicUser {
	return subsonicUser{
		Username:          a.Email,
		Email:             a.Email,
		ScrobblingEnabled: true,
		MaxBitRate:        a.MaxBitRate,
		AdminRole:         a.isAdmin(),
		SettingsRole:      a.CanChangePassword,
		DownloadRole:      a.CanDownload,
		PlaylistRole:      a.CanManagePlaylists,
		CommentRole:       a.CanRate,
		StreamRole:        a.CanStream,
		ShareRole:         a.CanShare,
		Folder:            []int{1},
	}
}

// usersPasswordParam reads a password the way the API sends it: plain, or
// "enc:" followed by its hex.
func usersPasswordParam(c *reqCtx) (string, error) {
	p := c.param("password")
	if p == "" {
		return "", envelope.MissingParam("password")
	}
	if hexed, ok := strings.CutPrefix(p, "enc:"); ok {
		decoded, err := hex.DecodeString(hexed)
		if err != nil {
			return "", &envelope.Error{
				Code:    envelope.CodeGeneric,
				Message: "Malformed password",
			}
		}
		p = string(decoded)
	}
	return p, nil
}

// getUser describes the caller's own account, or any account to an admin.
func getUser(c *reqCtx) sdk.Response {
	name := strings.TrimSpace(c.param("username"))
	if name == "" {
		return c.failErr(envelope.MissingParam("username"))
	}
	target, found, err := loadAccountByEmail(name)
	if err != nil {
		return c.failErr(err)
	}
	if !found {
		if !c.user.isAdmin() {
			// Whether an account exists is not a member's business.
			return c.failErr(envelope.NotAuthorized())
		}
		return c.failErr(envelope.NotFound())
	}
	if target.ID != c.user.ID && !c.user.isAdmin() {
		return c.failErr(envelope.NotAuthorized())
	}
	u := usersSubsonicUser(target)
	return c.ok(&body{User: &u})
}

func getUsers(c *reqCtx) sdk.Response {
	if !c.user.isAdmin() {
		return c.failErr(envelope.NotAuthorized())
	}
	rows, err := query(
		`SELECT l.user_id, l.email, u.role, u.display_name, ` + accountColumns + `, u.locked
		FROM streamline_user_logins l
		JOIN streamline_users u ON u.id = l.user_id
		LEFT JOIN accounts a ON a.user_id = l.user_id
		ORDER BY l.email`,
	)
	if err != nil {
		return c.failErr(err)
	}
	out := &subsonicUsers{User: make([]subsonicUser, 0, len(rows))}
	for _, r := range rows {
		out.User = append(out.User, usersSubsonicUser(scanAccount(r)))
	}
	return c.ok(&body{Users: out})
}

// usersManagedInStreamline answers the account-changing calls: accounts are
// managed by an admin in Streamline's web UI, never through a Subsonic
// credential, which is stored recoverable and travels in every request.
func usersManagedInStreamline(c *reqCtx) sdk.Response {
	return c.fail(
		envelope.CodeNotAuthorized,
		"Users are managed in Streamline's web UI",
	)
}

// changePassword sets the caller's own Subsonic password, when their account
// may. Nobody changes another account's through this API, admins included;
// the web login password is never this API's.
func changePassword(c *reqCtx) sdk.Response {
	name := strings.TrimSpace(c.param("username"))
	if name == "" {
		return c.failErr(envelope.MissingParam("username"))
	}
	if !strings.EqualFold(name, c.user.Email) {
		return c.failErr(envelope.NotAuthorized())
	}
	if err := c.require(permChangePassword); err != nil {
		return c.failErr(err)
	}
	password, err := usersPasswordParam(c)
	if err != nil {
		return c.failErr(err)
	}
	if len(password) < minPasswordLen {
		return c.fail(
			envelope.CodeGeneric,
			"Password must be at least 8 characters",
		)
	}
	if err := storePassword(c.user.ID, password); err != nil {
		return c.failErr(err)
	}
	return c.ok(nil)
}
