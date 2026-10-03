package platform

const (
	RoleOwner  = "owner"
	RoleMember = "member"
	RoleAgent  = "agent"
)

type Actor struct {
	Subject  string
	TenantID string
	Role     string
}
