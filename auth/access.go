package auth

import "systemsfoundry/platform"

const (
	ActionRead  = "read"
	ActionWrite = "write"
)

func IsOperator(actor platform.Actor, opsTenant string) bool {
	return opsTenant != "" && actor.Subject != "" && actor.TenantID == opsTenant && actor.Role == platform.RoleOwner
}

type DatabaseRole string

const (
	DatabaseWriter DatabaseRole = "app_writer"
	DatabaseReader DatabaseRole = "agent_reader"
)

func DatabaseRoleFor(actor platform.Actor) (DatabaseRole, bool) {
	switch actor.Role {
	case platform.RoleOwner, platform.RoleMember:
		return DatabaseWriter, true
	case platform.RoleAgent:
		return DatabaseReader, true
	default:
		return "", false
	}
}

func Can(actor platform.Actor, action, tenantID string) bool {
	if actor.Subject == "" || actor.TenantID == "" || tenantID == "" {
		return false
	}
	if actor.TenantID != tenantID {
		return false
	}
	switch actor.Role {
	case platform.RoleOwner, platform.RoleMember:
		return action == ActionRead || action == ActionWrite
	case platform.RoleAgent:
		return action == ActionRead
	default:
		return false
	}
}
