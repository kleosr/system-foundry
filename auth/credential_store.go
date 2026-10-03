package auth

import "time"

type credentialStore interface {
	putUser(row userRow) bool
	userByEmail(email string) (userRow, bool)
	userByID(id string) (userRow, bool)
	putSession(row sessionRow) error
	session(id string) (sessionRow, bool)
	deleteSession(id string) error
	putClient(row clientRow) error
	client(id string) (clientRow, bool)
	putCode(row codeRow) error
	takeCode(code string, now time.Time) (codeRow, bool)
	putRefresh(row refreshRow) error
	refreshByHash(hash string) (refreshRow, bool)
	markRefreshUsed(hash string) (refreshRow, bool)
	revokeFamily(family string) error
	revokeUserRefresh(userID string) error
}
