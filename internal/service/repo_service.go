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

// RepoService gère la logique métier des dépôts
type RepoService struct{}

// resolveOwnerGitUsername résout le GitUsername du propriétaire (user ou org)
// et retourne aussi le ownerID, ownerType
func (s *RepoService) resolveOwner(ctx context.Context, ownerName string) (ownerID uint, ownerType models.OwnerType, gitUsername string, err error) {
	// Chercher d'abord parmi les users
	var user models.User
	if e := orm.DB.Where("username = ?", ownerName).First(&user).Error; e == nil {
		return user.ID, models.OwnerTypeUser, user.GitUsername, nil
	}
	// Sinon chercher parmi les orgs
	var org models.Organization
	if e := orm.DB.Where("name = ?", ownerName).First(&org).Error; e == nil {
		return org.ID, models.OwnerTypeOrg, org.GitUsername, nil
	}
	return 0, "", "", status.Errorf(codes.NotFound, "owner '%s' not found", ownerName)
}

// canAccessRepo vérifie si l'utilisateur peut accéder au dépôt
func (s *RepoService) canAccessRepo(ctx context.Context, callerID uint, repo *models.Repository) bool {
	if !repo.IsPrivate {
		return true
	}
	if callerID == 0 {
		return false
	}
	// Admin global ?
	var caller models.User
	if err := orm.DB.First(&caller, callerID).Error; err == nil && caller.IsAdmin {
		return true
	}
	// Propriétaire direct ?
	if repo.OwnerType == models.OwnerTypeUser && repo.OwnerID == callerID {
		return true
	}
	// Membre de l'org propriétaire ?
	if repo.OwnerType == models.OwnerTypeOrg {
		var m models.OrgMembership
		if err := orm.DB.Where("organization_id = ? AND user_id = ?", repo.OwnerID, callerID).First(&m).Error; err == nil {
			return true
		}
	}
	// Collaborateur direct ?
	var collab models.RepoCollaborator
	if err := orm.DB.Where("repository_id = ? AND user_id = ?", repo.ID, callerID).First(&collab).Error; err == nil {
		return true
	}
	return false
}

// canWriteRepo vérifie si l'utilisateur peut écrire dans le dépôt
func (s *RepoService) canWriteRepo(ctx context.Context, callerID uint, repo *models.Repository) bool {
	if callerID == 0 {
		return false
	}
	var caller models.User
	if err := orm.DB.First(&caller, callerID).Error; err == nil && caller.IsAdmin {
		return true
	}
	if repo.OwnerType == models.OwnerTypeUser && repo.OwnerID == callerID {
		return true
	}
	if repo.OwnerType == models.OwnerTypeOrg {
		var m models.OrgMembership
		if err := orm.DB.Where("organization_id = ? AND user_id = ? AND role = ?", repo.OwnerID, callerID, models.OrgRoleOwner).First(&m).Error; err == nil {
			return true
		}
	}
	var collab models.RepoCollaborator
	if err := orm.DB.Where("repository_id = ? AND user_id = ? AND access_level IN ?", repo.ID, callerID, []string{"write", "admin"}).First(&collab).Error; err == nil {
		return true
	}
	return false
}

// getRepoByOwnerAndName retourne un dépôt par propriétaire + nom
func (s *RepoService) getRepoByOwnerAndName(ctx context.Context, ownerName, repoName string) (*models.Repository, error) {
	ownerID, ownerType, _, err := s.resolveOwner(ctx, ownerName)
	if err != nil {
		return nil, err
	}
	var repo models.Repository
	if e := orm.DB.Where("owner_id = ? AND owner_type = ? AND name = ?", ownerID, ownerType, repoName).First(&repo).Error; e != nil {
		if errors.Is(e, gorm.ErrRecordNotFound) {
			return nil, status.Error(codes.NotFound, "repository not found")
		}
		return nil, status.Error(codes.Internal, "database error")
	}
	return &repo, nil
}

// ownerName retourne le nom d'affichage du propriétaire d'un dépôt
func (s *RepoService) ownerName(repo *models.Repository) string {
	if repo.OwnerType == models.OwnerTypeUser {
		var u models.User
		if err := orm.DB.First(&u, repo.OwnerID).Error; err == nil {
			return u.Username
		}
	} else {
		var o models.Organization
		if err := orm.DB.First(&o, repo.OwnerID).Error; err == nil {
			return o.Name
		}
	}
	return "unknown"
}

// CreateRepository crée un dépôt dans la DB et dans soft-serve
func (s *RepoService) CreateRepository(ctx context.Context, callerID uint, name, description string, isPrivate bool, orgName *string) (*models.Repository, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return nil, status.Error(codes.InvalidArgument, "repository name is required")
	}

	var ownerID uint
	var ownerType models.OwnerType
	var gitOwnerUsername string

	if orgName != nil && *orgName != "" {
		// Création sous une org
		var org models.Organization
		if err := orm.DB.Where("name = ?", *orgName).First(&org).Error; err != nil {
			return nil, status.Error(codes.NotFound, "organization not found")
		}
		// Vérifier que le caller est membre
		var m models.OrgMembership
		if err := orm.DB.Where("organization_id = ? AND user_id = ?", org.ID, callerID).First(&m).Error; err != nil {
			return nil, status.Error(codes.PermissionDenied, "you are not a member of this organization")
		}
		ownerID = org.ID
		ownerType = models.OwnerTypeOrg
		gitOwnerUsername = org.GitUsername
	} else {
		// Création sous l'utilisateur courant
		var user models.User
		if err := orm.DB.First(&user, callerID).Error; err != nil {
			return nil, status.Error(codes.NotFound, "user not found")
		}
		ownerID = user.ID
		ownerType = models.OwnerTypeUser
		gitOwnerUsername = user.GitUsername
	}

	// Vérifier doublon en DB
	var existing models.Repository
	if err := orm.DB.Where("owner_id = ? AND owner_type = ? AND name = ?", ownerID, ownerType, name).First(&existing).Error; err == nil {
		return nil, status.Error(codes.AlreadyExists, "repository already exists")
	}

	// Créer dans soft-serve
	gitRepoName := fmt.Sprintf("%s/%s", gitOwnerUsername, name)
	_, err := gitClient.GitClient.CreateRepository(ctx, &ssgrpc.CreateRepositoryRequest{
		Name:        gitRepoName,
		Description: description,
		Username:    gitOwnerUsername,
		Private:     isPrivate,
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create git repository: %v", err)
	}

	repo := &models.Repository{
		Name:          name,
		Description:   description,
		IsPrivate:     isPrivate,
		DefaultBranch: "main",
		GitRepoName:   gitRepoName,
		OwnerID:       ownerID,
		OwnerType:     ownerType,
	}
	if err := orm.DB.Create(repo).Error; err != nil {
		_ = gitClient.GitClient.DeleteRepository(ctx, &ssgrpc.DeleteRepositoryRequest{Name: gitRepoName})
		return nil, status.Errorf(codes.Internal, "failed to persist repository: %v", err)
	}
	SeedDefaultLabels(repo.ID)
	return repo, nil
}

// GetRepository retourne un dépôt et vérifie les droits d'accès
func (s *RepoService) GetRepository(ctx context.Context, callerID uint, ownerName, repoName string) (*models.Repository, error) {
	repo, err := s.getRepoByOwnerAndName(ctx, ownerName, repoName)
	if err != nil {
		return nil, err
	}
	if !s.canAccessRepo(ctx, callerID, repo) {
		return nil, status.Error(codes.PermissionDenied, "access denied")
	}
	return repo, nil
}

// ListRepositories retourne les dépôts publics (ou tous si admin)
func (s *RepoService) ListRepositories(ctx context.Context, callerID uint, page, perPage int, includePrivate bool) ([]models.Repository, int64, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 100 {
		perPage = 30
	}
	offset := (page - 1) * perPage

	query := orm.DB.Model(&models.Repository{})
	if !includePrivate {
		query = query.Where("is_private = ?", false)
	}

	var total int64
	query.Count(&total)
	var repos []models.Repository
	if err := query.Offset(offset).Limit(perPage).Find(&repos).Error; err != nil {
		return nil, 0, status.Error(codes.Internal, "database error")
	}
	return repos, total, nil
}

// ListUserRepositories retourne les dépôts d'un utilisateur
func (s *RepoService) ListUserRepositories(ctx context.Context, callerID uint, username string, page, perPage int) ([]models.Repository, int64, error) {
	var user models.User
	if err := orm.DB.Where("username = ?", username).First(&user).Error; err != nil {
		return nil, 0, status.Error(codes.NotFound, "user not found")
	}
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 100 {
		perPage = 30
	}
	offset := (page - 1) * perPage

	query := orm.DB.Where("owner_id = ? AND owner_type = ?", user.ID, models.OwnerTypeUser)
	// Masquer les privés si ce n'est pas soi-même
	if callerID != user.ID {
		query = query.Where("is_private = ?", false)
	}

	var total int64
	query.Model(&models.Repository{}).Count(&total)
	var repos []models.Repository
	if err := query.Offset(offset).Limit(perPage).Find(&repos).Error; err != nil {
		return nil, 0, status.Error(codes.Internal, "database error")
	}
	return repos, total, nil
}

// ListOrgRepositories retourne les dépôts d'une organisation
func (s *RepoService) ListOrgRepositories(ctx context.Context, callerID uint, orgName string, page, perPage int) ([]models.Repository, int64, error) {
	var org models.Organization
	if err := orm.DB.Where("name = ?", orgName).First(&org).Error; err != nil {
		return nil, 0, status.Error(codes.NotFound, "organization not found")
	}
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 100 {
		perPage = 30
	}
	offset := (page - 1) * perPage

	// Vérifier si caller est membre
	isMember := false
	if callerID != 0 {
		var m models.OrgMembership
		if err := orm.DB.Where("organization_id = ? AND user_id = ?", org.ID, callerID).First(&m).Error; err == nil {
			isMember = true
		}
	}

	query := orm.DB.Where("owner_id = ? AND owner_type = ?", org.ID, models.OwnerTypeOrg)
	if !isMember {
		query = query.Where("is_private = ?", false)
	}

	var total int64
	query.Model(&models.Repository{}).Count(&total)
	var repos []models.Repository
	if err := query.Offset(offset).Limit(perPage).Find(&repos).Error; err != nil {
		return nil, 0, status.Error(codes.Internal, "database error")
	}
	return repos, total, nil
}

// UpdateRepository met à jour les métadonnées d'un dépôt
func (s *RepoService) UpdateRepository(ctx context.Context, callerID uint, ownerName, repoName string, description *string, isPrivate *bool, defaultBranch *string) (*models.Repository, error) {
	repo, err := s.getRepoByOwnerAndName(ctx, ownerName, repoName)
	if err != nil {
		return nil, err
	}
	if !s.canWriteRepo(ctx, callerID, repo) {
		return nil, status.Error(codes.PermissionDenied, "write access required")
	}

	updates := map[string]interface{}{}
	gitUpdates := &ssgrpc.UpdateRepositoryRequest{Name: repo.GitRepoName}
	needsGitUpdate := false

	if description != nil {
		updates["description"] = *description
		gitUpdates.Description = description
		needsGitUpdate = true
	}
	if isPrivate != nil {
		updates["is_private"] = *isPrivate
		gitUpdates.IsPrivate = isPrivate
		needsGitUpdate = true
	}
	if defaultBranch != nil {
		updates["default_branch"] = *defaultBranch
	}

	if err := orm.DB.Model(repo).Updates(updates).Error; err != nil {
		return nil, status.Error(codes.Internal, "failed to update repository")
	}
	if needsGitUpdate {
		_, _ = gitClient.GitClient.UpdateRepository(ctx, gitUpdates)
	}
	if defaultBranch != nil {
		_, _ = gitClient.GitClient.SetDefaultBranch(ctx, &ssgrpc.SetDefaultBranchRequest{
			RepoName:   repo.GitRepoName,
			BranchName: *defaultBranch,
		})
	}
	return repo, nil
}

// DeleteRepository supprime un dépôt
func (s *RepoService) DeleteRepository(ctx context.Context, callerID uint, ownerName, repoName string) error {
	repo, err := s.getRepoByOwnerAndName(ctx, ownerName, repoName)
	if err != nil {
		return err
	}
	if !s.canWriteRepo(ctx, callerID, repo) {
		return status.Error(codes.PermissionDenied, "write access required")
	}

	// Supprimer côté git
	_ = gitClient.GitClient.DeleteRepository(ctx, &ssgrpc.DeleteRepositoryRequest{Name: repo.GitRepoName})

	// Supprimer collaborators
	orm.DB.Where("repository_id = ?", repo.ID).Delete(&models.RepoCollaborator{})

	return orm.DB.Delete(repo).Error
}

// RenameRepository renomme un dépôt
func (s *RepoService) RenameRepository(ctx context.Context, callerID uint, ownerName, oldName, newName string) (*models.Repository, error) {
	repo, err := s.getRepoByOwnerAndName(ctx, ownerName, oldName)
	if err != nil {
		return nil, err
	}
	if !s.canWriteRepo(ctx, callerID, repo) {
		return nil, status.Error(codes.PermissionDenied, "write access required")
	}

	_, _, gitOwnerUsername, rerr := s.resolveOwner(ctx, ownerName)
	if rerr != nil {
		return nil, rerr
	}

	newGitRepoName := fmt.Sprintf("%s/%s", gitOwnerUsername, newName)
	if _, err := gitClient.GitClient.RenameRepository(ctx, &ssgrpc.RenameRepositoryRequest{
		OldName: repo.GitRepoName,
		NewName: newGitRepoName,
	}); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to rename git repository: %v", err)
	}

	if err := orm.DB.Model(repo).Updates(map[string]interface{}{
		"name":          newName,
		"git_repo_name": newGitRepoName,
	}).Error; err != nil {
		return nil, status.Error(codes.Internal, "failed to rename repository in database")
	}
	return repo, nil
}

// ─── Content proxies ────────────────────────────────────────────────────────

// isEmptyRepoErr détecte les erreurs soft-serve typiques d'un dépôt vide
// (aucun commit, HEAD inexistant, révision introuvable, références manquantes).
func isEmptyRepoErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, needle := range []string{
		"reference does not exist",
		"revision does not exist",
		"failed to get references",
		"failed to get HEAD",
		"tree not found",
		"object not found",
	} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}

// GetRepositoryTree retourne l'arbre de fichiers d'un dépôt
func (s *RepoService) GetRepositoryTree(ctx context.Context, callerID uint, ownerName, repoName string, ref, path *string) (*ssgrpc.GetTreeResponse, error) {
	repo, err := s.GetRepository(ctx, callerID, ownerName, repoName)
	if err != nil {
		return nil, err
	}
	req := &ssgrpc.GetTreeRequest{RepoName: repo.GitRepoName}
	if ref != nil {
		req.Ref = ref
	}
	if path != nil {
		req.Path = path
	}
	res, err := gitClient.GitClient.GetTree(ctx, req)
	if err != nil {
		if isEmptyRepoErr(err) {
			return &ssgrpc.GetTreeResponse{}, nil
		}
		return nil, err
	}
	return res, nil
}

// GetFileBlob retourne le contenu d'un fichier
func (s *RepoService) GetFileBlob(ctx context.Context, callerID uint, ownerName, repoName, path string, ref *string) (*ssgrpc.GetBlobResponse, error) {
	repo, err := s.GetRepository(ctx, callerID, ownerName, repoName)
	if err != nil {
		return nil, err
	}
	req := &ssgrpc.GetBlobRequest{RepoName: repo.GitRepoName, Path: path}
	if ref != nil {
		req.Ref = ref
	}
	return gitClient.GitClient.GetBlob(ctx, req)
}

// ListBranches retourne les branches d'un dépôt
func (s *RepoService) ListBranches(ctx context.Context, callerID uint, ownerName, repoName string) (*ssgrpc.GetBranchesResponse, error) {
	repo, err := s.GetRepository(ctx, callerID, ownerName, repoName)
	if err != nil {
		return nil, err
	}
	res, err := gitClient.GitClient.GetBranches(ctx, &ssgrpc.GetBranchesRequest{RepoName: repo.GitRepoName})
	if err != nil {
		if isEmptyRepoErr(err) {
			return &ssgrpc.GetBranchesResponse{}, nil
		}
		return nil, err
	}
	return res, nil
}

// CreateBranch crée une nouvelle branche dans un dépôt
func (s *RepoService) CreateBranch(ctx context.Context, callerID uint, ownerName, repoName, branchName, source string) (*ssgrpc.Branch, error) {
	repo, err := s.getRepoByOwnerAndName(ctx, ownerName, repoName)
	if err != nil {
		return nil, err
	}
	if !s.canWriteRepo(ctx, callerID, repo) {
		return nil, status.Error(codes.PermissionDenied, "write access required")
	}
	return gitClient.GitClient.CreateBranch(ctx, &ssgrpc.CreateBranchRequest{
		RepoName:   repo.GitRepoName,
		BranchName: branchName,
		Source:     source,
	})
}

// DeleteBranch supprime une branche dans un dépôt
func (s *RepoService) DeleteBranch(ctx context.Context, callerID uint, ownerName, repoName, branchName string) error {
	repo, err := s.getRepoByOwnerAndName(ctx, ownerName, repoName)
	if err != nil {
		return err
	}
	if !s.canWriteRepo(ctx, callerID, repo) {
		return status.Error(codes.PermissionDenied, "write access required")
	}
	return gitClient.GitClient.DeleteBranch(ctx, &ssgrpc.DeleteBranchRequest{
		RepoName:   repo.GitRepoName,
		BranchName: branchName,
	})
}

// GetDefaultBranch retourne la branche par défaut d'un dépôt
func (s *RepoService) GetDefaultBranch(ctx context.Context, callerID uint, ownerName, repoName string) (*ssgrpc.DefaultBranchResponse, error) {
	repo, err := s.GetRepository(ctx, callerID, ownerName, repoName)
	if err != nil {
		return nil, err
	}
	res, err := gitClient.GitClient.GetDefaultBranch(ctx, &ssgrpc.GetDefaultBranchRequest{RepoName: repo.GitRepoName})
	if err != nil {
		if isEmptyRepoErr(err) {
			// Repo vide : retourner la valeur stockée en DB
			return &ssgrpc.DefaultBranchResponse{BranchName: repo.DefaultBranch}, nil
		}
		return nil, err
	}
	return res, nil
}

// SetDefaultBranch change la branche par défaut d'un dépôt
func (s *RepoService) SetDefaultBranch(ctx context.Context, callerID uint, ownerName, repoName, branchName string) (*ssgrpc.DefaultBranchResponse, error) {
	repo, err := s.getRepoByOwnerAndName(ctx, ownerName, repoName)
	if err != nil {
		return nil, err
	}
	if !s.canWriteRepo(ctx, callerID, repo) {
		return nil, status.Error(codes.PermissionDenied, "write access required")
	}
	res, err := gitClient.GitClient.SetDefaultBranch(ctx, &ssgrpc.SetDefaultBranchRequest{
		RepoName:   repo.GitRepoName,
		BranchName: branchName,
	})
	if err != nil {
		return nil, err
	}
	orm.DB.Model(repo).Update("default_branch", branchName)
	return res, nil
}

// ListTags retourne les tags d'un dépôt
func (s *RepoService) ListTags(ctx context.Context, callerID uint, ownerName, repoName string) (*ssgrpc.ListTagsResponse, error) {
	repo, err := s.GetRepository(ctx, callerID, ownerName, repoName)
	if err != nil {
		return nil, err
	}
	res, err := gitClient.GitClient.ListTags(ctx, &ssgrpc.ListTagsRequest{RepoName: repo.GitRepoName})
	if err != nil {
		if isEmptyRepoErr(err) {
			return &ssgrpc.ListTagsResponse{}, nil
		}
		return nil, err
	}
	return res, nil
}

// CreateTag crée un tag dans un dépôt
func (s *RepoService) CreateTag(ctx context.Context, callerID uint, ownerName, repoName, tagName, target string, message *string) (*ssgrpc.TagDetail, error) {
	repo, err := s.getRepoByOwnerAndName(ctx, ownerName, repoName)
	if err != nil {
		return nil, err
	}
	if !s.canWriteRepo(ctx, callerID, repo) {
		return nil, status.Error(codes.PermissionDenied, "write access required")
	}
	req := &ssgrpc.CreateTagRequest{
		RepoName: repo.GitRepoName,
		TagName:  tagName,
		Target:   target,
	}
	if message != nil {
		req.Message = message
	}
	return gitClient.GitClient.CreateTag(ctx, req)
}

// DeleteTag supprime un tag
func (s *RepoService) DeleteTag(ctx context.Context, callerID uint, ownerName, repoName, tagName string) error {
	repo, err := s.getRepoByOwnerAndName(ctx, ownerName, repoName)
	if err != nil {
		return err
	}
	if !s.canWriteRepo(ctx, callerID, repo) {
		return status.Error(codes.PermissionDenied, "write access required")
	}
	return gitClient.GitClient.DeleteTag(ctx, &ssgrpc.DeleteTagRequest{RepoName: repo.GitRepoName, TagName: tagName})
}

// ListCommits retourne les commits d'un dépôt
func (s *RepoService) ListCommits(ctx context.Context, callerID uint, ownerName, repoName string, ref *string, limit, page int32) (*ssgrpc.ListCommitsResponse, error) {
	repo, err := s.GetRepository(ctx, callerID, ownerName, repoName)
	if err != nil {
		return nil, err
	}
	req := &ssgrpc.ListCommitsRequest{RepoName: repo.GitRepoName}
	if ref != nil {
		req.Ref = ref
	}
	req.Limit = &limit
	req.Page = &page
	res, err := gitClient.GitClient.ListCommits(ctx, req)
	if err != nil {
		if isEmptyRepoErr(err) {
			return &ssgrpc.ListCommitsResponse{}, nil
		}
		return nil, err
	}
	return res, nil
}

// GetCommit retourne le détail d'un commit
func (s *RepoService) GetCommit(ctx context.Context, callerID uint, ownerName, repoName, sha string) (*ssgrpc.CommitDetail, error) {
	repo, err := s.GetRepository(ctx, callerID, ownerName, repoName)
	if err != nil {
		return nil, err
	}
	return gitClient.GitClient.GetCommit(ctx, &ssgrpc.GetCommitRequest{RepoName: repo.GitRepoName, Sha: sha})
}

// GetRepositoryStats retourne les statistiques d'un dépôt
func (s *RepoService) GetRepositoryStats(ctx context.Context, callerID uint, ownerName, repoName string) (*ssgrpc.RepositoryStatsResponse, error) {
	repo, err := s.GetRepository(ctx, callerID, ownerName, repoName)
	if err != nil {
		return nil, err
	}
	res, err := gitClient.GitClient.GetRepositoryStats(ctx, &ssgrpc.GetRepositoryStatsRequest{RepoName: repo.GitRepoName})
	if err != nil {
		if isEmptyRepoErr(err) {
			return &ssgrpc.RepositoryStatsResponse{}, nil
		}
		return nil, err
	}
	return res, nil
}

// GetCloneURLs retourne les URLs de clone d'un dépôt
func (s *RepoService) GetCloneURLs(ctx context.Context, callerID uint, ownerName, repoName string) (*ssgrpc.CloneURLsResponse, error) {
	repo, err := s.GetRepository(ctx, callerID, ownerName, repoName)
	if err != nil {
		return nil, err
	}
	return gitClient.GitClient.GetCloneURLs(ctx, &ssgrpc.GetCloneURLsRequest{RepoName: repo.GitRepoName})
}

// CompareBranches compare deux branches
func (s *RepoService) CompareBranches(ctx context.Context, callerID uint, ownerName, repoName, base, head string) (*ssgrpc.CompareResponse, error) {
	repo, err := s.GetRepository(ctx, callerID, ownerName, repoName)
	if err != nil {
		return nil, err
	}
	return gitClient.GitClient.CompareBranches(ctx, &ssgrpc.CompareBranchesRequest{
		RepoName:   repo.GitRepoName,
		BaseBranch: base,
		HeadBranch: head,
	})
}

// CompareCommits compare deux commits
func (s *RepoService) CompareCommits(ctx context.Context, callerID uint, ownerName, repoName, baseSha, headSha string) (*ssgrpc.CompareResponse, error) {
	repo, err := s.GetRepository(ctx, callerID, ownerName, repoName)
	if err != nil {
		return nil, err
	}
	return gitClient.GitClient.CompareCommits(ctx, &ssgrpc.CompareCommitsRequest{
		RepoName: repo.GitRepoName,
		BaseSha:  baseSha,
		HeadSha:  headSha,
	})
}

// GetFileHistory retourne l'historique d'un fichier
func (s *RepoService) GetFileHistory(ctx context.Context, callerID uint, ownerName, repoName, path string, ref *string, limit int32) (*ssgrpc.GetFileHistoryResponse, error) {
	repo, err := s.GetRepository(ctx, callerID, ownerName, repoName)
	if err != nil {
		return nil, err
	}
	req := &ssgrpc.GetFileHistoryRequest{RepoName: repo.GitRepoName, Path: path, Limit: &limit}
	if ref != nil {
		req.Ref = ref
	}
	return gitClient.GitClient.GetFileHistory(ctx, req)
}

// SearchCommits recherche des commits
func (s *RepoService) SearchCommits(ctx context.Context, callerID uint, ownerName, repoName, query string, author, ref *string, limit int32) (*ssgrpc.ListCommitsResponse, error) {
	repo, err := s.GetRepository(ctx, callerID, ownerName, repoName)
	if err != nil {
		return nil, err
	}
	req := &ssgrpc.SearchCommitsRequest{
		RepoName: repo.GitRepoName,
		Query:    query,
		Limit:    &limit,
	}
	if author != nil {
		req.Author = author
	}
	if ref != nil {
		req.Ref = ref
	}
	res, err := gitClient.GitClient.SearchCommits(ctx, req)
	if err != nil {
		if isEmptyRepoErr(err) {
			return &ssgrpc.ListCommitsResponse{}, nil
		}
		return nil, err
	}
	return res, nil
}

// CheckPath vérifie si un chemin existe dans le dépôt
func (s *RepoService) CheckPath(ctx context.Context, callerID uint, ownerName, repoName, path string, ref *string) (*ssgrpc.CheckPathResponse, error) {
	repo, err := s.GetRepository(ctx, callerID, ownerName, repoName)
	if err != nil {
		return nil, err
	}
	req := &ssgrpc.CheckPathRequest{RepoName: repo.GitRepoName, Path: path}
	if ref != nil {
		req.Ref = ref
	}
	res, err := gitClient.GitClient.CheckPath(ctx, req)
	if err != nil {
		if isEmptyRepoErr(err) {
			return &ssgrpc.CheckPathResponse{Exists: false}, nil
		}
		return nil, err
	}
	return res, nil
}

// ─── Collaborateurs ─────────────────────────────────────────────────────────

// ListCollaborators retourne les collaborateurs d'un dépôt
func (s *RepoService) ListCollaborators(ctx context.Context, callerID uint, ownerName, repoName string) ([]models.RepoCollaborator, error) {
	repo, err := s.GetRepository(ctx, callerID, ownerName, repoName)
	if err != nil {
		return nil, err
	}
	var collabs []models.RepoCollaborator
	if err := orm.DB.Preload("User").Where("repository_id = ?", repo.ID).Find(&collabs).Error; err != nil {
		return nil, status.Error(codes.Internal, "database error")
	}
	return collabs, nil
}

// AddCollaborator ajoute un collaborateur à un dépôt
func (s *RepoService) AddCollaborator(ctx context.Context, callerID uint, ownerName, repoName, username, accessLevel string) error {
	repo, err := s.getRepoByOwnerAndName(ctx, ownerName, repoName)
	if err != nil {
		return err
	}
	if !s.canWriteRepo(ctx, callerID, repo) {
		return status.Error(codes.PermissionDenied, "write access required")
	}
	var targetUser models.User
	if err := orm.DB.Where("username = ?", username).First(&targetUser).Error; err != nil {
		return status.Error(codes.NotFound, "user not found")
	}
	collab := &models.RepoCollaborator{
		RepositoryID: repo.ID,
		UserID:       targetUser.ID,
		AccessLevel:  accessLevel,
	}
	if err := orm.DB.Create(collab).Error; err != nil {
		return status.Error(codes.AlreadyExists, "user is already a collaborator")
	}
	// Ajouter aussi côté soft-serve
	_ = gitClient.GitClient.AddCollaborator(ctx, &ssgrpc.AddCollaboratorRequest{
		RepoName:    repo.GitRepoName,
		Username:    targetUser.GitUsername,
		AccessLevel: ssgrpc.AccessLevel_READ_WRITE,
	})
	return nil
}

// RemoveCollaborator retire un collaborateur d'un dépôt
func (s *RepoService) RemoveCollaborator(ctx context.Context, callerID uint, ownerName, repoName, username string) error {
	repo, err := s.getRepoByOwnerAndName(ctx, ownerName, repoName)
	if err != nil {
		return err
	}
	if !s.canWriteRepo(ctx, callerID, repo) {
		return status.Error(codes.PermissionDenied, "write access required")
	}
	var targetUser models.User
	if err := orm.DB.Where("username = ?", username).First(&targetUser).Error; err != nil {
		return status.Error(codes.NotFound, "user not found")
	}
	orm.DB.Where("repository_id = ? AND user_id = ?", repo.ID, targetUser.ID).Delete(&models.RepoCollaborator{})
	_ = gitClient.GitClient.RemoveCollaborator(ctx, &ssgrpc.RemoveCollaboratorRequest{
		RepoName: repo.GitRepoName,
		Username: targetUser.GitUsername,
	})
	return nil
}

// UpdateCollaborator met à jour le niveau d'accès d'un collaborateur
func (s *RepoService) UpdateCollaborator(ctx context.Context, callerID uint, ownerName, repoName, username, accessLevel string) error {
	repo, err := s.getRepoByOwnerAndName(ctx, ownerName, repoName)
	if err != nil {
		return err
	}
	if !s.canWriteRepo(ctx, callerID, repo) {
		return status.Error(codes.PermissionDenied, "write access required")
	}
	var targetUser models.User
	if err := orm.DB.Where("username = ?", username).First(&targetUser).Error; err != nil {
		return status.Error(codes.NotFound, "user not found")
	}
	if err := orm.DB.Model(&models.RepoCollaborator{}).
		Where("repository_id = ? AND user_id = ?", repo.ID, targetUser.ID).
		Update("access_level", accessLevel).Error; err != nil {
		return status.Error(codes.Internal, "failed to update collaborator")
	}
	// Sync soft-serve: remove + re-add
	_ = gitClient.GitClient.RemoveCollaborator(ctx, &ssgrpc.RemoveCollaboratorRequest{
		RepoName: repo.GitRepoName,
		Username: targetUser.GitUsername,
	})
	_ = gitClient.GitClient.AddCollaborator(ctx, &ssgrpc.AddCollaboratorRequest{
		RepoName:    repo.GitRepoName,
		Username:    targetUser.GitUsername,
		AccessLevel: ssgrpc.AccessLevel_READ_WRITE,
	})
	return nil
}

// OwnerName est une méthode publique pour récupérer le nom du propriétaire
func (s *RepoService) OwnerName(repo *models.Repository) string {
	return s.ownerName(repo)
}
