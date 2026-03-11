package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Gyt-project/backend-api/internal/gitClient"
	"github.com/Gyt-project/backend-api/internal/orm"
	"github.com/Gyt-project/backend-api/pkg/models"
	ssgrpc "github.com/Gyt-project/soft-serve/pkg/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

// OrgService gère la logique métier des organisations
type OrgService struct{}

// gitOrgUsername retourne le username git conventionnel pour une org
func gitOrgUsername(orgName string) string {
	return "org-" + orgName
}

// CreateOrganization crée une organisation dans la DB et un faux user dans soft-serve
func (s *OrgService) CreateOrganization(ctx context.Context, ownerID uint, name, displayName, description string) (*models.Organization, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return nil, status.Error(codes.InvalidArgument, "organization name is required")
	}

	// Vérifier que le nom n'est pas déjà pris
	var existing models.Organization
	if err := orm.DB.Where("name = ?", name).First(&existing).Error; !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, status.Error(codes.AlreadyExists, "organization name already taken")
	}

	gitUsername := gitOrgUsername(name)
	// Créer un faux utilisateur soft-serve pour l'org
	gitUser, err := gitClient.GitClient.CreateUser(ctx, &ssgrpc.CreateUserRequest{
		Username:   gitUsername,
		Admin:      false,
		PublicKeys: []string{},
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create org git user: %v", err)
	}

	org := &models.Organization{
		Name:        name,
		DisplayName: displayName,
		Description: description,
		GitUsername: gitUsername,
		GitID:       fmt.Sprintf("%d", gitUser.GetId()),
		OwnerID:     ownerID,
	}
	if err := orm.DB.Create(org).Error; err != nil {
		_ = gitClient.GitClient.DeleteUser(ctx, &ssgrpc.DeleteUserRequest{Username: gitUsername})
		return nil, status.Errorf(codes.Internal, "failed to create organization: %v", err)
	}

	// Ajouter le créateur comme owner
	membership := models.OrgMembership{
		OrganizationID: org.ID,
		UserID:         ownerID,
		Role:           models.OrgRoleOwner,
	}
	if err := orm.DB.Create(&membership).Error; err != nil {
		return nil, status.Error(codes.Internal, "failed to add owner membership")
	}

	return org, nil
}

// GetOrganization retourne une org par son nom
func (s *OrgService) GetOrganization(ctx context.Context, name string) (*models.Organization, error) {
	var org models.Organization
	if err := orm.DB.Preload("Owner").Where("name = ?", name).First(&org).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, status.Error(codes.NotFound, "organization not found")
		}
		return nil, status.Error(codes.Internal, "database error")
	}
	return &org, nil
}

// ListOrganizations retourne une liste paginée d'organisations
func (s *OrgService) ListOrganizations(ctx context.Context, page, perPage int) ([]models.Organization, int64, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 100 {
		perPage = 30
	}
	offset := (page - 1) * perPage

	var orgs []models.Organization
	var total int64
	orm.DB.Model(&models.Organization{}).Count(&total)
	if err := orm.DB.Preload("Owner").Offset(offset).Limit(perPage).Find(&orgs).Error; err != nil {
		return nil, 0, status.Error(codes.Internal, "database error")
	}
	return orgs, total, nil
}

// ListUserOrganizations retourne les organisations d'un utilisateur
func (s *OrgService) ListUserOrganizations(ctx context.Context, username string) ([]models.Organization, error) {
	var user models.User
	if err := orm.DB.Where("username = ?", username).First(&user).Error; err != nil {
		return nil, status.Error(codes.NotFound, "user not found")
	}

	var memberships []models.OrgMembership
	if err := orm.DB.Preload("Organization.Owner").Where("user_id = ?", user.ID).Find(&memberships).Error; err != nil {
		return nil, status.Error(codes.Internal, "database error")
	}

	orgs := make([]models.Organization, 0, len(memberships))
	for _, m := range memberships {
		orgs = append(orgs, m.Organization)
	}
	return orgs, nil
}

// UpdateOrganization met à jour les métadonnées d'une organisation
func (s *OrgService) UpdateOrganization(ctx context.Context, callerID uint, name string, displayName, description, avatarURL *string) (*models.Organization, error) {
	var org models.Organization
	if err := orm.DB.Preload("Owner").Where("name = ?", name).First(&org).Error; err != nil {
		return nil, status.Error(codes.NotFound, "organization not found")
	}

	// Seuls owner ou admin peuvent modifier
	if !s.isOrgOwnerOrAdmin(ctx, callerID, &org) {
		return nil, status.Error(codes.PermissionDenied, "only org owners can update the organization")
	}

	updates := map[string]interface{}{}
	if displayName != nil {
		updates["display_name"] = *displayName
	}
	if description != nil {
		updates["description"] = *description
	}
	if avatarURL != nil {
		updates["avatar_url"] = *avatarURL
	}
	if err := orm.DB.Model(&org).Updates(updates).Error; err != nil {
		return nil, status.Error(codes.Internal, "failed to update organization")
	}
	return &org, nil
}

// DeleteOrganization supprime une org, ses repos et son faux user git
func (s *OrgService) DeleteOrganization(ctx context.Context, callerID uint, name string) error {
	var org models.Organization
	if err := orm.DB.Preload("Owner").Where("name = ?", name).First(&org).Error; err != nil {
		return status.Error(codes.NotFound, "organization not found")
	}

	if !s.isOrgOwnerOrAdmin(ctx, callerID, &org) {
		return status.Error(codes.PermissionDenied, "only org owners can delete the organization")
	}

	// Supprimer les repos git de l'org
	var repos []models.Repository
	orm.DB.Where("owner_id = ? AND owner_type = ?", org.ID, models.OwnerTypeOrg).Find(&repos)
	for _, r := range repos {
		_ = gitClient.GitClient.DeleteRepository(ctx, &ssgrpc.DeleteRepositoryRequest{Name: r.GitRepoName})
	}
	// Supprimer les repos en DB
	orm.DB.Where("owner_id = ? AND owner_type = ?", org.ID, models.OwnerTypeOrg).Delete(&models.Repository{})

	// Supprimer memberships
	orm.DB.Where("organization_id = ?", org.ID).Delete(&models.OrgMembership{})

	// Supprimer le faux user soft-serve
	_ = gitClient.GitClient.DeleteUser(ctx, &ssgrpc.DeleteUserRequest{Username: org.GitUsername})

	return orm.DB.Delete(&org).Error
}

// ListOrgMembers retourne les membres d'une organisation
func (s *OrgService) ListOrgMembers(ctx context.Context, orgName string) ([]models.OrgMembership, error) {
	var org models.Organization
	if err := orm.DB.Where("name = ?", orgName).First(&org).Error; err != nil {
		return nil, status.Error(codes.NotFound, "organization not found")
	}

	var memberships []models.OrgMembership
	if err := orm.DB.Preload("User").Where("organization_id = ?", org.ID).Find(&memberships).Error; err != nil {
		return nil, status.Error(codes.Internal, "database error")
	}
	return memberships, nil
}

// AddOrgMember ajoute un utilisateur à une organisation
func (s *OrgService) AddOrgMember(ctx context.Context, callerID uint, orgName, username, role string) (*models.OrgMembership, error) {
	var org models.Organization
	if err := orm.DB.Where("name = ?", orgName).First(&org).Error; err != nil {
		return nil, status.Error(codes.NotFound, "organization not found")
	}
	if !s.isOrgOwnerOrAdmin(ctx, callerID, &org) {
		return nil, status.Error(codes.PermissionDenied, "only org owners can add members")
	}

	var targetUser models.User
	if err := orm.DB.Where("username = ?", username).First(&targetUser).Error; err != nil {
		return nil, status.Error(codes.NotFound, "user not found")
	}

	// Vérifier doublon
	var existing models.OrgMembership
	if err := orm.DB.Where("organization_id = ? AND user_id = ?", org.ID, targetUser.ID).First(&existing).Error; err == nil {
		return nil, status.Error(codes.AlreadyExists, "user is already a member")
	}

	memberRole := models.OrgMemberRole(role)
	if memberRole != models.OrgRoleOwner && memberRole != models.OrgRoleMember {
		memberRole = models.OrgRoleMember
	}

	membership := &models.OrgMembership{
		OrganizationID: org.ID,
		UserID:         targetUser.ID,
		Role:           memberRole,
	}
	if err := orm.DB.Create(membership).Error; err != nil {
		return nil, status.Error(codes.Internal, "failed to add member")
	}
	membership.User = targetUser
	membership.Organization = org
	return membership, nil
}

// UpdateOrgMember met à jour le rôle d'un membre
func (s *OrgService) UpdateOrgMember(ctx context.Context, callerID uint, orgName, username, role string) (*models.OrgMembership, error) {
	var org models.Organization
	if err := orm.DB.Where("name = ?", orgName).First(&org).Error; err != nil {
		return nil, status.Error(codes.NotFound, "organization not found")
	}
	if !s.isOrgOwnerOrAdmin(ctx, callerID, &org) {
		return nil, status.Error(codes.PermissionDenied, "only org owners can update member roles")
	}

	var targetUser models.User
	if err := orm.DB.Where("username = ?", username).First(&targetUser).Error; err != nil {
		return nil, status.Error(codes.NotFound, "user not found")
	}

	var membership models.OrgMembership
	if err := orm.DB.Where("organization_id = ? AND user_id = ?", org.ID, targetUser.ID).First(&membership).Error; err != nil {
		return nil, status.Error(codes.NotFound, "membership not found")
	}

	memberRole := models.OrgMemberRole(role)
	if memberRole != models.OrgRoleOwner && memberRole != models.OrgRoleMember {
		return nil, status.Error(codes.InvalidArgument, "invalid role, must be 'owner' or 'member'")
	}
	membership.Role = memberRole
	if err := orm.DB.Save(&membership).Error; err != nil {
		return nil, status.Error(codes.Internal, "failed to update membership")
	}
	membership.User = targetUser
	return &membership, nil
}

// RemoveOrgMember retire un utilisateur d'une organisation
func (s *OrgService) RemoveOrgMember(ctx context.Context, callerID uint, orgName, username string) error {
	var org models.Organization
	if err := orm.DB.Where("name = ?", orgName).First(&org).Error; err != nil {
		return status.Error(codes.NotFound, "organization not found")
	}
	if !s.isOrgOwnerOrAdmin(ctx, callerID, &org) {
		return status.Error(codes.PermissionDenied, "only org owners can remove members")
	}

	var targetUser models.User
	if err := orm.DB.Where("username = ?", username).First(&targetUser).Error; err != nil {
		return status.Error(codes.NotFound, "user not found")
	}

	return orm.DB.Where("organization_id = ? AND user_id = ?", org.ID, targetUser.ID).Delete(&models.OrgMembership{}).Error
}

// GetOrgMembership retourne le membership d'un user dans une org
func (s *OrgService) GetOrgMembership(ctx context.Context, orgName, username string) (*models.OrgMembership, error) {
	var org models.Organization
	if err := orm.DB.Where("name = ?", orgName).First(&org).Error; err != nil {
		return nil, status.Error(codes.NotFound, "organization not found")
	}
	var user models.User
	if err := orm.DB.Where("username = ?", username).First(&user).Error; err != nil {
		return nil, status.Error(codes.NotFound, "user not found")
	}

	var membership models.OrgMembership
	if err := orm.DB.Preload("User").Where("organization_id = ? AND user_id = ?", org.ID, user.ID).First(&membership).Error; err != nil {
		return nil, status.Error(codes.NotFound, "membership not found")
	}
	return &membership, nil
}

// CountOrgMembers compte les membres d'une org
func (s *OrgService) CountOrgMembers(orgID uint) int32 {
	var count int64
	orm.DB.Model(&models.OrgMembership{}).Where("organization_id = ?", orgID).Count(&count)
	return int32(count)
}

// CountOrgRepos compte les dépôts d'une org
func (s *OrgService) CountOrgRepos(orgID uint) int32 {
	var count int64
	orm.DB.Model(&models.Repository{}).Where("owner_id = ? AND owner_type = ?", orgID, models.OwnerTypeOrg).Count(&count)
	return int32(count)
}

// isOrgOwnerOrAdmin vérifie si l'utilisateur est owner de l'org ou admin global
func (s *OrgService) isOrgOwnerOrAdmin(ctx context.Context, userID uint, org *models.Organization) bool {
	// Admin global
	var caller models.User
	if err := orm.DB.First(&caller, userID).Error; err == nil && caller.IsAdmin {
		return true
	}
	// Owner de l'org
	var m models.OrgMembership
	if err := orm.DB.Where("organization_id = ? AND user_id = ? AND role = ?", org.ID, userID, models.OrgRoleOwner).First(&m).Error; err == nil {
		return true
	}
	return false
}
