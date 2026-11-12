package main

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/datahearth/streamline/extensions/sdk"
	"github.com/streamline-hq/streamline-subsonic/creds"
)

const (
	cardCredential = "credential"
	cardSettings   = "settings"

	// generatedPasswordLen is the length of the password the credential card
	// generates.
	generatedPasswordLen = 24

	maxBitRateCeiling = 320
)

// accountCards is what the manifest declares: the credential card Streamline
// draws on the account page, and the permissions card an admin edits.
func accountCards() []sdk.AccountCard {
	return []sdk.AccountCard{
		{
			ID:       cardCredential,
			Kind:     sdk.CardCredential,
			Title:    "Subsonic",
			Mount:    "/rest",
			Username: "email",
		},
		{
			ID:    cardSettings,
			Kind:  sdk.CardSettings,
			Title: "Subsonic apps",
			Fields: []sdk.CardField{
				{ID: "can_stream", Kind: sdk.FieldSwitch, Label: "Stream"},
				{ID: "can_download", Kind: sdk.FieldSwitch, Label: "Download"},
				{
					ID:    "can_manage_playlists",
					Kind:  sdk.FieldSwitch,
					Label: "Create and edit playlists",
				},
				{
					ID:    "can_share",
					Kind:  sdk.FieldSwitch,
					Label: "Create share links",
				},
				{ID: "can_rate", Kind: sdk.FieldSwitch, Label: "Rate"},
				{
					ID:    "can_change_password",
					Kind:  sdk.FieldSwitch,
					Label: "Change their own Subsonic password",
				},
				{
					ID:    "max_bit_rate",
					Kind:  sdk.FieldNumber,
					Label: "Maximum bit rate (kbps, 0 for no limit)",
					Min:   0,
					Max:   maxBitRateCeiling,
				},
			},
		},
	}
}

// cards answers /cards/<id>, which Streamline calls for the account page and
// for an admin editing another account.
func cards(r sdk.Request) sdk.Response {
	var (
		resp sdk.Response
		err  error
	)
	switch strings.TrimSuffix(r.Path, "/") {
	case "/cards/" + cardCredential:
		resp, err = credentialCard(r)
	case "/cards/" + cardSettings:
		resp, err = settingsCard(r)
	default:
		return notFoundResponse()
	}
	if err != nil {
		sdk.Log(
			"error",
			"a subsonic card failed",
			"path",
			r.Path,
			"error",
			err.Error(),
		)
		return textResponse(http.StatusInternalServerError, "internal error")
	}
	return resp
}

func jsonResponse(v any) (sdk.Response, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return sdk.Response{}, err
	}
	return sdk.Response{
		Status:  http.StatusOK,
		Headers: map[string][]string{"Content-Type": {"application/json"}},
		Body:    b,
	}, nil
}

func credentialCard(r sdk.Request) (sdk.Response, error) {
	if r.Caller == nil {
		return textResponse(http.StatusUnauthorized, "unauthorized"), nil
	}
	switch strings.ToUpper(r.Method) {
	case http.MethodGet:
		return credentialStatus(r.Caller.ID)
	case http.MethodPost:
		return rotatePassword(r.Caller.ID)
	case http.MethodDelete:
		return revokePassword(r.Caller.ID)
	}
	return textResponse(http.StatusMethodNotAllowed, "method not allowed"), nil
}

func optional(t string) *string {
	if t == "" {
		return nil
	}
	return &t
}

func credentialStatus(userID uint32) (sdk.Response, error) {
	out := struct {
		Set        bool    `json:"set"`
		CreatedAt  *string `json:"created_at"`
		LastUsedAt *string `json:"last_used_at"`
		LastClient *string `json:"last_client"`
	}{}
	r, found, err := queryRow(
		`SELECT password, password_created_at, last_used_at, last_client
		FROM accounts WHERE user_id = ?`,
		userID,
	)
	if err != nil {
		return sdk.Response{}, err
	}
	if found && r.str(0) != "" {
		out.Set = true
		out.CreatedAt = optional(optTimestamp(r.time(1)))
		out.LastUsedAt = optional(optTimestamp(r.time(2)))
		out.LastClient = optional(r.str(3))
	}
	return jsonResponse(out)
}

func rotatePassword(userID uint32) (sdk.Response, error) {
	secret, err := creds.Generate(generatedPasswordLen)
	if err != nil {
		return sdk.Response{}, err
	}
	if err := storePassword(userID, secret); err != nil {
		return sdk.Response{}, err
	}
	return jsonResponse(struct {
		Secret string `json:"secret"`
	}{secret})
}

// revokePassword ends the account's Subsonic access and keeps its
// permissions.
func revokePassword(userID uint32) (sdk.Response, error) {
	_, err := exec(
		`UPDATE accounts SET password = '', password_created_at = NULL,
			last_used_at = NULL, last_client = ''
		WHERE user_id = ?`,
		userID,
	)
	if err != nil {
		return sdk.Response{}, err
	}
	return sdk.Response{Status: http.StatusNoContent}, nil
}

// settingsCard reads and writes the permissions of r.TargetUser, the account
// an admin is editing. Streamline has already checked a write against the
// card's fields.
func settingsCard(r sdk.Request) (sdk.Response, error) {
	if r.TargetUser == 0 {
		return textResponse(http.StatusBadRequest, "no account"), nil
	}
	switch strings.ToUpper(r.Method) {
	case http.MethodGet:
		return readSettings(r.TargetUser)
	case http.MethodPut:
		return writeSettings(r.TargetUser, r.Body)
	}
	return textResponse(http.StatusMethodNotAllowed, "method not allowed"), nil
}

func readSettings(userID uint32) (sdk.Response, error) {
	acc, found, err := loadAccount(userID)
	if err != nil {
		return sdk.Response{}, err
	}
	if !found {
		return textResponse(http.StatusNotFound, "no such account"), nil
	}
	return jsonResponse(map[string]any{"values": map[string]any{
		"can_stream":           acc.CanStream,
		"can_download":         acc.CanDownload,
		"can_manage_playlists": acc.CanManagePlaylists,
		"can_share":            acc.CanShare,
		"can_rate":             acc.CanRate,
		"can_change_password":  acc.CanChangePassword,
		"max_bit_rate":         acc.MaxBitRate,
	}})
}

// writeSettings stores the fields the body names and leaves the others as
// they are: each column keeps its value when its argument is NULL.
func writeSettings(userID uint32, raw []byte) (sdk.Response, error) {
	var in struct {
		Values struct {
			Stream          *bool `json:"can_stream"`
			Download        *bool `json:"can_download"`
			ManagePlaylists *bool `json:"can_manage_playlists"`
			Share           *bool `json:"can_share"`
			Rate            *bool `json:"can_rate"`
			ChangePassword  *bool `json:"can_change_password"`
			MaxBitRate      *int  `json:"max_bit_rate"`
		} `json:"values"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return textResponse(http.StatusBadRequest, "unreadable body"), nil
	}
	v := in.Values
	err := tx(func() error {
		if _, err := exec(
			"INSERT OR IGNORE INTO accounts(user_id) VALUES(?)", userID,
		); err != nil {
			return err
		}
		_, err := exec(
			`UPDATE accounts SET
				can_stream = COALESCE(?, can_stream),
				can_download = COALESCE(?, can_download),
				can_manage_playlists = COALESCE(?, can_manage_playlists),
				can_share = COALESCE(?, can_share),
				can_rate = COALESCE(?, can_rate),
				can_change_password = COALESCE(?, can_change_password),
				max_bit_rate = COALESCE(?, max_bit_rate)
			WHERE user_id = ?`,
			flag(v.Stream), flag(v.Download), flag(v.ManagePlaylists),
			flag(v.Share), flag(v.Rate), flag(v.ChangePassword),
			v.MaxBitRate, userID,
		)
		return err
	})
	if err != nil {
		return sdk.Response{}, err
	}
	return sdk.Response{Status: http.StatusNoContent}, nil
}

// flag is a switch as the column stores it; nil leaves the column alone.
func flag(b *bool) any {
	switch {
	case b == nil:
		return nil
	case *b:
		return 1
	}
	return 0
}
