package iam

import (
	"context"
	"errors"
	"fmt"

	"github.com/hashicorp/watchtower/internal/db"
	"github.com/hashicorp/watchtower/internal/iam/store"
	"google.golang.org/protobuf/proto"
)

// Roles are granted permissions and assignable to User and Groups
type Role struct {
	*store.Role
	tableName string `gorm:"-"`
}

// ensure that Group implements the interfaces of: Resource, Clonable, and db.VetForWriter
var _ Resource = (*Role)(nil)
var _ Clonable = (*Role)(nil)
var _ db.VetForWriter = (*Role)(nil)

// NewRole creates a new in memory role with a scope (project/organization)
// options include: withDescripion, WithName
func NewRole(scopeId string, opt ...Option) (*Role, error) {
	opts := getOpts(opt...)
	withName := opts.withName
	withDescription := opts.withDescription
	if scopeId == "" {
		return nil, errors.New("error the role scope id is unset")
	}
	publicId, err := db.NewPublicId("r")
	if err != nil {
		return nil, fmt.Errorf("error generating public id %w for new role", err)
	}
	r := &Role{
		Role: &store.Role{
			PublicId:    publicId,
			ScopeId:     scopeId,
			Name:        withName,
			Description: withDescription,
		},
	}
	return r, nil
}

func allocRole() Role {
	return Role{
		Role: &store.Role{},
	}
}

// Clone creates a clone of the Role
func (r *Role) Clone() interface{} {
	cp := proto.Clone(r.Role)
	return &Role{
		Role: cp.(*store.Role),
	}
}

// AssignedRoles returns a list of principal roles (Users and Groups) for the Role.
func (role *Role) AssignedRoles(ctx context.Context, r db.Reader) ([]AssignedRole, error) {
	viewRoles := []*assignedRoleView{}
	if err := r.SearchWhere(
		ctx,
		&viewRoles,
		"role_id = ? and type in(?, ?)",
		role.PublicId, UserRoleType.String(), GroupRoleType.String()); err != nil {
		return nil, fmt.Errorf("error getting assigned roles %w", err)
	}

	pRoles := []AssignedRole{}
	for _, vr := range viewRoles {
		switch vr.Type {
		case UserRoleType.String():
			pr := &UserRole{
				UserRole: &store.UserRole{
					CreateTime:  vr.CreateTime,
					RoleId:      vr.RoleId,
					PrincipalId: vr.PrincipalId,
				},
			}
			pRoles = append(pRoles, pr)
		case GroupRoleType.String():
			pr := &GroupRole{
				GroupRole: &store.GroupRole{
					CreateTime:  vr.CreateTime,
					RoleId:      vr.RoleId,
					PrincipalId: vr.PrincipalId,
				},
			}
			pRoles = append(pRoles, pr)
		default:
			return nil, fmt.Errorf("error unsupported role type: %s", vr.Type)
		}
	}
	return pRoles, nil
}

// VetForWrite implements db.VetForWrite() interface
func (role *Role) VetForWrite(ctx context.Context, r db.Reader, opType db.OpType, opt ...db.Option) error {
	if role.PublicId == "" {
		return errors.New("error public id is empty string for role write")
	}
	if err := validateScopeForWrite(ctx, r, role, opType, opt...); err != nil {
		return err
	}
	return nil
}

func (u *Role) validScopeTypes() []ScopeType {
	return []ScopeType{OrganizationScope, ProjectScope}
}

// Getscope returns the scope for the Role
func (role *Role) GetScope(ctx context.Context, r db.Reader) (*Scope, error) {
	return LookupScope(ctx, r, role)
}

// ResourceType returns the type of the Role
func (*Role) ResourceType() ResourceType { return ResourceTypeRole }

// Actions returns the  available actions for Role
func (*Role) Actions() map[string]Action {
	return CrudActions()
}

// TableName returns the tablename to override the default gorm table name
func (r *Role) TableName() string {
	if r.tableName != "" {
		return r.tableName
	}
	return "iam_role"
}

// SetTableName sets the tablename and satisfies the ReplayableMessage interface
func (r *Role) SetTableName(n string) {
	if n != "" {
		r.tableName = n
	}
}