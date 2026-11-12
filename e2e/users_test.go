package e2e

import (
	"encoding/hex"
	"net/url"
	"slices"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("user endpoints", Label("e2e"), func() {
	var (
		member *testUser
		admin  *testUser
	)

	BeforeEach(func() {
		member = app.subsonicUser("users-member")
		admin = app.newUserWithRole("users-admin", "admin")
		app.setSubsonicPassword(admin, "admin-pass-1")
	})

	It("lets a member read only their own account and an admin read any", func() {
		me := obj(member.call("getUser", url.Values{
			"username": {upper(member.Email)},
		}), "user")
		Expect(me["adminRole"]).To(BeFalse())
		Expect(me["streamRole"]).To(BeTrue())
		Expect(me["username"]).To(Equal(member.Email))

		Expect(errorCode(member.callError("getUser", url.Values{
			"username": {admin.Email},
		}))).To(Equal(50))
		// A missing account reads the same as someone else's.
		Expect(errorCode(member.callError("getUser", url.Values{
			"username": {"ghost-nobody@example.com"},
		}))).To(Equal(50))
		Expect(errorCode(member.callError("getUser", nil))).To(Equal(10))
		Expect(errorCode(member.callError("getUsers", nil))).To(Equal(50))

		other := obj(admin.call("getUser", url.Values{
			"username": {member.Email},
		}), "user")
		Expect(other["username"]).To(Equal(member.Email))
		Expect(errorCode(admin.callError("getUser", url.Values{
			"username": {"ghost-nobody@example.com"},
		}))).To(Equal(70))

		all := list(obj(admin.call("getUsers", nil), "users"), "user")
		got := make([]string, 0, len(all))
		for _, it := range all {
			got = append(got, it["username"].(string))
		}
		Expect(got).To(ContainElements(member.Email, admin.Email))
		Expect(slices.IsSorted(got)).To(BeTrue())
	})

	It("refuses every account change, even to an admin", func() {
		for endpoint, params := range map[string]url.Values{
			"createUser": {
				"username": {"bob"}, "email": {"bob@example.com"},
				"password": {"password1"}, "adminRole": {"true"},
			},
			"updateUser": {"username": {member.Email}, "adminRole": {"true"}},
			"deleteUser": {"username": {member.Email}},
		} {
			e := admin.callError(endpoint, params)
			Expect(errorCode(e)).To(Equal(50), endpoint)
			Expect(e["message"]).To(ContainSubstring("web UI"), endpoint)
		}
		member.call("ping", nil)
		got := obj(admin.call("getUser", url.Values{
			"username": {member.Email},
		}), "user")
		Expect(got["adminRole"]).To(BeFalse())
	})

	It("changes only the caller's own Subsonic password", func() {
		member.call("changePassword", url.Values{
			"username": {upper(member.Email)},
			"password": {"enc:" + hex.EncodeToString([]byte("fresh-pass"))},
		})
		member.Password = "fresh-pass"
		member.call("ping", nil)

		// Not someone else's, admin or not.
		Expect(errorCode(admin.callError("changePassword", url.Values{
			"username": {member.Email}, "password": {"admin-set-1"},
		}))).To(Equal(50))
		member.call("ping", nil)

		Expect(errorCode(member.callError("changePassword", url.Values{
			"username": {member.Email}, "password": {"short"},
		}))).To(Equal(0))
		Expect(errorCode(member.callError("changePassword", url.Values{
			"username": {member.Email},
		}))).To(Equal(10))
		Expect(errorCode(member.callError("changePassword", url.Values{
			"username": {member.Email}, "password": {"enc:zz"},
		}))).To(Equal(0))
		Expect(errorCode(member.callError("changePassword", url.Values{
			"password": {"another-pass"},
		}))).To(Equal(10))
		member.call("ping", nil)

		app.setSettings(member, map[string]any{"can_change_password": false})
		Expect(errorCode(member.callError("changePassword", url.Values{
			"username": {member.Email}, "password": {"another-pass"},
		}))).To(Equal(50))
		member.call("ping", nil)
	})

	It("reports the permissions the settings card set", func() {
		app.setSettings(member, map[string]any{
			"can_share":    false,
			"max_bit_rate": 128,
		})
		me := obj(member.call("getUser", url.Values{
			"username": {member.Email},
		}), "user")
		Expect(me["shareRole"]).To(BeFalse())
		Expect(me["downloadRole"]).To(BeTrue())
		Expect(me["maxBitRate"]).To(BeEquivalentTo(128))
	})

	It("answers an account with the wrong password with error 40", func() {
		member.Password = "nope"
		Expect(errorCode(member.callError("getUser", url.Values{
			"username": {member.Email},
		}))).To(Equal(40))
	})
})

var _ = Describe("library scan", Label("e2e"), func() {
	It("reports the tracks holding a file", func() {
		app.sharedLibrary()
		u := app.subsonicUser("scan")
		st := obj(u.call("getScanStatus", nil), "scanStatus")
		Expect(st).To(HaveKey("scanning"))
		Expect(st["count"]).To(BeNumerically(">=", 6))
	})

	It("starts a scan for an admin only", func() {
		app.sharedLibrary()
		member := app.subsonicUser("scan-member")
		Expect(errorCode(member.callError("startScan", nil))).To(Equal(50))

		admin := app.newUserWithRole("scan-admin", "admin")
		app.setSubsonicPassword(admin, "admin-pass-2")
		st := obj(admin.call("startScan", nil), "scanStatus")
		Expect(st).To(HaveKey("scanning"))
		Expect(st["count"]).To(BeNumerically(">=", 6))
	})
})

func upper(s string) string { return strings.ToUpper(s) }
