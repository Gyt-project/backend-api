// Package server contient les handlers gRPC pour le service GYT.
// NOTE: Ce fichier implémente GytServiceServer généré par protoc.
// Comme le proto n'est pas encore compilé, on déclare l'interface manuellement.
// Une fois `protoc` exécuté, remplacer par l'interface générée.
package server

import (
	"context"

	"github.com/Gyt-project/backend-api/internal/auth"
	"github.com/Gyt-project/backend-api/internal/service"
	pb "github.com/Gyt-project/backend-api/pkg/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// GytServer implémente pb.GytServiceServer
type GytServer struct {
	pb.UnimplementedGytServiceServer
	Users *service.UserService
	Repos *service.RepoService
	Orgs  *service.OrgService
}

func NewGytServer() *GytServer {
	return &GytServer{
		Users: &service.UserService{},
		Repos: &service.RepoService{},
		Orgs:  &service.OrgService{},
	}
}

// ─── Auth ─────────────────────────────────────────────────────────────────────

func (s *GytServer) Register(ctx context.Context, req *pb.RegisterRequest) (*pb.AuthResponse, error) {
	user, accessToken, refreshToken, err := s.Users.Register(ctx, req.GetUsername(), req.GetEmail(), req.GetPassword(), req.GetDisplayName())
	if err != nil {
		return nil, err
	}
	return &pb.AuthResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    int64(auth.AccessTokenDuration.Seconds()),
		User:         userToProto(user),
	}, nil
}

func (s *GytServer) Login(ctx context.Context, req *pb.LoginRequest) (*pb.AuthResponse, error) {
	user, accessToken, refreshToken, err := s.Users.Login(ctx, req.GetLogin(), req.GetPassword())
	if err != nil {
		return nil, err
	}
	return &pb.AuthResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    int64(auth.AccessTokenDuration.Seconds()),
		User:         userToProto(user),
	}, nil
}

func (s *GytServer) RefreshToken(ctx context.Context, req *pb.RefreshTokenRequest) (*pb.AuthResponse, error) {
	user, accessToken, refreshToken, err := s.Users.RefreshToken(ctx, req.GetRefreshToken())
	if err != nil {
		return nil, err
	}
	return &pb.AuthResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    int64(auth.AccessTokenDuration.Seconds()),
		User:         userToProto(user),
	}, nil
}

// ─── Users ────────────────────────────────────────────────────────────────────

func (s *GytServer) GetUser(ctx context.Context, req *pb.GetUserRequest) (*pb.UserResponse, error) {
	user, err := s.Users.GetByUsername(ctx, req.GetUsername())
	if err != nil {
		return nil, err
	}
	return userToProto(user), nil
}

func (s *GytServer) GetCurrentUser(ctx context.Context, _ *emptypb.Empty) (*pb.UserResponse, error) {
	userID, err := auth.ExtractUserID(ctx)
	if err != nil {
		return nil, err
	}
	user, err := s.Users.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	return userToProto(user), nil
}

func (s *GytServer) UpdateUser(ctx context.Context, req *pb.UpdateUserRequest) (*pb.UserResponse, error) {
	userID, err := auth.ExtractUserID(ctx)
	if err != nil {
		return nil, err
	}
	user, err := s.Users.UpdateUser(ctx, userID, req.DisplayName, req.Bio, req.AvatarUrl, req.Email, req.Password)
	if err != nil {
		return nil, err
	}
	return userToProto(user), nil
}

func (s *GytServer) DeleteUser(ctx context.Context, req *pb.DeleteUserRequest) (*emptypb.Empty, error) {
	callerID, _ := auth.ExtractUserID(ctx)
	isAdmin := auth.ExtractIsAdmin(ctx)
	target, err := s.Users.GetByUsername(ctx, req.GetUsername())
	if err != nil {
		return nil, err
	}
	if !isAdmin && callerID != target.ID {
		return nil, status.Error(codes.PermissionDenied, "cannot delete another user's account")
	}
	if err := s.Users.DeleteUser(ctx, req.GetUsername()); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

func (s *GytServer) ListUsers(ctx context.Context, req *pb.ListUsersRequest) (*pb.ListUsersResponse, error) {
	page, perPage := int(req.GetPage()), int(req.GetPerPage())
	users, total, err := s.Users.ListUsers(ctx, page, perPage)
	if err != nil {
		return nil, err
	}
	resp := &pb.ListUsersResponse{Total: int32(total), Page: int32(page), PerPage: int32(perPage)}
	for i := range users {
		resp.Users = append(resp.Users, userToProto(&users[i]))
	}
	return resp, nil
}

// SSH Keys

func (s *GytServer) ListSSHKeys(ctx context.Context, req *pb.ListSSHKeysRequest) (*pb.ListSSHKeysResponse, error) {
	keys, err := s.Users.ListSSHKeys(ctx, req.GetUsername())
	if err != nil {
		return nil, err
	}
	resp := &pb.ListSSHKeysResponse{}
	for i := range keys {
		resp.Keys = append(resp.Keys, &pb.SSHKeyResponse{
			Id:        uint64(keys[i].ID),
			Name:      keys[i].Name,
			PublicKey: keys[i].PublicKey,
			CreatedAt: timestamppb.New(keys[i].CreatedAt),
		})
	}
	return resp, nil
}

func (s *GytServer) AddSSHKey(ctx context.Context, req *pb.AddSSHKeyRequest) (*pb.SSHKeyResponse, error) {
	userID, err := auth.ExtractUserID(ctx)
	if err != nil {
		return nil, err
	}
	key, err := s.Users.AddSSHKey(ctx, userID, req.GetName(), req.GetPublicKey())
	if err != nil {
		return nil, err
	}
	return &pb.SSHKeyResponse{
		Id:        uint64(key.ID),
		Name:      key.Name,
		PublicKey: key.PublicKey,
		CreatedAt: timestamppb.New(key.CreatedAt),
	}, nil
}

func (s *GytServer) DeleteSSHKey(ctx context.Context, req *pb.DeleteSSHKeyRequest) (*emptypb.Empty, error) {
	userID, err := auth.ExtractUserID(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.Users.DeleteSSHKey(ctx, userID, uint(req.GetKeyId())); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

// ─── Repositories ─────────────────────────────────────────────────────────────

func (s *GytServer) CreateRepository(ctx context.Context, req *pb.CreateRepoRequest) (*pb.RepositoryResponse, error) {
	callerID, err := auth.ExtractUserID(ctx)
	if err != nil {
		return nil, err
	}
	repo, err := s.Repos.CreateRepository(ctx, callerID, req.GetName(), req.GetDescription(), req.GetIsPrivate(), req.OrgName)
	if err != nil {
		return nil, err
	}
	return repoToProto(repo, s.Repos.OwnerName(repo)), nil
}

func (s *GytServer) GetRepository(ctx context.Context, req *pb.GetRepoRequest) (*pb.RepositoryResponse, error) {
	callerID, _ := auth.ExtractUserID(ctx)
	repo, err := s.Repos.GetRepository(ctx, callerID, req.GetOwner(), req.GetName())
	if err != nil {
		return nil, err
	}
	return repoToProto(repo, s.Repos.OwnerName(repo)), nil
}

func (s *GytServer) ListRepositories(ctx context.Context, req *pb.ListReposRequest) (*pb.ListReposResponse, error) {
	callerID, _ := auth.ExtractUserID(ctx)
	isAdmin := auth.ExtractIsAdmin(ctx)
	includePrivate := req.GetIncludePrivate() && isAdmin
	repos, total, err := s.Repos.ListRepositories(ctx, callerID, int(req.GetPage()), int(req.GetPerPage()), includePrivate)
	if err != nil {
		return nil, err
	}
	return reposToListResponse(repos, total, req.GetPage(), req.GetPerPage(), s.Repos), nil
}

func (s *GytServer) ListUserRepositories(ctx context.Context, req *pb.ListUserReposRequest) (*pb.ListReposResponse, error) {
	callerID, _ := auth.ExtractUserID(ctx)
	repos, total, err := s.Repos.ListUserRepositories(ctx, callerID, req.GetUsername(), int(req.GetPage()), int(req.GetPerPage()))
	if err != nil {
		return nil, err
	}
	return reposToListResponse(repos, total, req.GetPage(), req.GetPerPage(), s.Repos), nil
}

func (s *GytServer) ListOrgRepositories(ctx context.Context, req *pb.ListOrgReposRequest) (*pb.ListReposResponse, error) {
	callerID, _ := auth.ExtractUserID(ctx)
	repos, total, err := s.Repos.ListOrgRepositories(ctx, callerID, req.GetOrgName(), int(req.GetPage()), int(req.GetPerPage()))
	if err != nil {
		return nil, err
	}
	return reposToListResponse(repos, total, req.GetPage(), req.GetPerPage(), s.Repos), nil
}

func (s *GytServer) UpdateRepository(ctx context.Context, req *pb.UpdateRepoRequest) (*pb.RepositoryResponse, error) {
	callerID, err := auth.ExtractUserID(ctx)
	if err != nil {
		return nil, err
	}
	repo, err := s.Repos.UpdateRepository(ctx, callerID, req.GetOwner(), req.GetName(), req.Description, req.IsPrivate, req.DefaultBranch)
	if err != nil {
		return nil, err
	}
	return repoToProto(repo, s.Repos.OwnerName(repo)), nil
}

func (s *GytServer) DeleteRepository(ctx context.Context, req *pb.DeleteRepoRequest) (*emptypb.Empty, error) {
	callerID, err := auth.ExtractUserID(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.Repos.DeleteRepository(ctx, callerID, req.GetOwner(), req.GetName()); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

func (s *GytServer) RenameRepository(ctx context.Context, req *pb.RenameRepoRequest) (*pb.RepositoryResponse, error) {
	callerID, err := auth.ExtractUserID(ctx)
	if err != nil {
		return nil, err
	}
	repo, err := s.Repos.RenameRepository(ctx, callerID, req.GetOwner(), req.GetOldName(), req.GetNewName())
	if err != nil {
		return nil, err
	}
	return repoToProto(repo, s.Repos.OwnerName(repo)), nil
}

// Repository Content

func (s *GytServer) GetRepositoryTree(ctx context.Context, req *pb.GetRepoTreeRequest) (*pb.RepoTreeResponse, error) {
	callerID, _ := auth.ExtractUserID(ctx)
	result, err := s.Repos.GetRepositoryTree(ctx, callerID, req.GetOwner(), req.GetName(), req.Ref, req.Path)
	if err != nil {
		return nil, err
	}
	resp := &pb.RepoTreeResponse{Ref: result.GetRef()}
	for _, e := range result.GetEntries() {
		resp.Entries = append(resp.Entries, &pb.TreeEntryResponse{
			Name:        e.GetName(),
			Path:        e.GetPath(),
			Mode:        e.GetMode(),
			Size:        e.GetSize(),
			IsDir:       e.GetIsDir(),
			IsSubmodule: e.GetIsSubmodule(),
		})
	}
	return resp, nil
}

func (s *GytServer) GetFileBlob(ctx context.Context, req *pb.GetFileBlobRequest) (*pb.FileBlobResponse, error) {
	callerID, _ := auth.ExtractUserID(ctx)
	result, err := s.Repos.GetFileBlob(ctx, callerID, req.GetOwner(), req.GetName(), req.GetPath(), req.Ref)
	if err != nil {
		return nil, err
	}
	return &pb.FileBlobResponse{
		Content:  result.GetContent(),
		Size:     result.GetSize(),
		IsBinary: result.GetIsBinary(),
		Path:     result.GetPath(),
	}, nil
}

func (s *GytServer) ListBranches(ctx context.Context, req *pb.ListBranchesRequest) (*pb.ListBranchesResponse, error) {
	callerID, _ := auth.ExtractUserID(ctx)
	result, err := s.Repos.ListBranches(ctx, callerID, req.GetOwner(), req.GetName())
	if err != nil {
		return nil, err
	}
	resp := &pb.ListBranchesResponse{}
	for _, b := range result.GetBranches() {
		resp.Branches = append(resp.Branches, &pb.BranchResponse{
			Name:      b.GetName(),
			FullName:  b.GetFullName(),
			CommitSha: b.GetCommitSha(),
		})
	}
	return resp, nil
}

func (s *GytServer) GetDefaultBranch(ctx context.Context, req *pb.GetDefaultBranchRequest) (*pb.DefaultBranchResponse, error) {
	callerID, _ := auth.ExtractUserID(ctx)
	result, err := s.Repos.GetDefaultBranch(ctx, callerID, req.GetOwner(), req.GetName())
	if err != nil {
		return nil, err
	}
	return &pb.DefaultBranchResponse{BranchName: result.GetBranchName()}, nil
}

func (s *GytServer) SetDefaultBranch(ctx context.Context, req *pb.SetDefaultBranchRequest) (*pb.DefaultBranchResponse, error) {
	callerID, err := auth.ExtractUserID(ctx)
	if err != nil {
		return nil, err
	}
	result, err := s.Repos.SetDefaultBranch(ctx, callerID, req.GetOwner(), req.GetName(), req.GetBranchName())
	if err != nil {
		return nil, err
	}
	return &pb.DefaultBranchResponse{BranchName: result.GetBranchName()}, nil
}

func (s *GytServer) ListTags(ctx context.Context, req *pb.ListTagsRequest) (*pb.ListTagsResponse, error) {
	callerID, _ := auth.ExtractUserID(ctx)
	result, err := s.Repos.ListTags(ctx, callerID, req.GetOwner(), req.GetName())
	if err != nil {
		return nil, err
	}
	resp := &pb.ListTagsResponse{}
	for _, t := range result.GetTags() {
		resp.Tags = append(resp.Tags, &pb.TagResponse{
			Name:      t.GetName(),
			FullName:  t.GetFullName(),
			CommitSha: t.GetCommitSha(),
			Message:   t.Message,
		})
	}
	return resp, nil
}

func (s *GytServer) CreateTag(ctx context.Context, req *pb.CreateTagRequest) (*pb.TagDetailResponse, error) {
	callerID, err := auth.ExtractUserID(ctx)
	if err != nil {
		return nil, err
	}
	result, err := s.Repos.CreateTag(ctx, callerID, req.GetOwner(), req.GetName(), req.GetTagName(), req.GetTarget(), req.Message)
	if err != nil {
		return nil, err
	}
	resp := &pb.TagDetailResponse{
		Tag: &pb.TagResponse{
			Name:      result.GetTag().GetName(),
			FullName:  result.GetTag().GetFullName(),
			CommitSha: result.GetTag().GetCommitSha(),
			Message:   result.GetTag().Message,
		},
	}
	if c := result.GetCommit(); c != nil {
		resp.Commit = commitToProto(c)
	}
	return resp, nil
}

func (s *GytServer) DeleteTag(ctx context.Context, req *pb.DeleteTagRequest) (*emptypb.Empty, error) {
	callerID, err := auth.ExtractUserID(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.Repos.DeleteTag(ctx, callerID, req.GetOwner(), req.GetName(), req.GetTagName()); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

func (s *GytServer) ListCommits(ctx context.Context, req *pb.ListCommitsRequest) (*pb.ListCommitsResponse, error) {
	callerID, _ := auth.ExtractUserID(ctx)
	result, err := s.Repos.ListCommits(ctx, callerID, req.GetOwner(), req.GetName(), req.Ref, req.GetLimit(), req.GetPage())
	if err != nil {
		return nil, err
	}
	resp := &pb.ListCommitsResponse{
		Page:    result.GetPage(),
		PerPage: result.GetPerPage(),
		HasMore: result.GetHasMore(),
	}
	for _, c := range result.GetCommits() {
		resp.Commits = append(resp.Commits, commitToProto(c))
	}
	return resp, nil
}

func (s *GytServer) GetCommit(ctx context.Context, req *pb.GetCommitRequest) (*pb.CommitDetailResponse, error) {
	callerID, _ := auth.ExtractUserID(ctx)
	result, err := s.Repos.GetCommit(ctx, callerID, req.GetOwner(), req.GetName(), req.GetSha())
	if err != nil {
		return nil, err
	}
	resp := &pb.CommitDetailResponse{
		Commit:         commitToProto(result.GetCommit()),
		TotalAdditions: result.GetTotalAdditions(),
		TotalDeletions: result.GetTotalDeletions(),
		FilesChanged:   result.GetFilesChanged(),
		Patch:          result.GetPatch(),
	}
	for _, f := range result.GetFiles() {
		resp.Files = append(resp.Files, &pb.FileDiffResponse{
			Path:      f.GetPath(),
			Additions: f.GetAdditions(),
			Deletions: f.GetDeletions(),
			Status:    f.GetStatus(),
			OldPath:   f.OldPath,
		})
	}
	return resp, nil
}

func (s *GytServer) GetRepositoryStats(ctx context.Context, req *pb.GetRepoStatsRequest) (*pb.RepoStatsResponse, error) {
	callerID, _ := auth.ExtractUserID(ctx)
	result, err := s.Repos.GetRepositoryStats(ctx, callerID, req.GetOwner(), req.GetName())
	if err != nil {
		return nil, err
	}
	return &pb.RepoStatsResponse{
		SizeBytes:        result.GetSizeBytes(),
		CommitCount:      result.GetCommitCount(),
		BranchCount:      result.GetBranchCount(),
		TagCount:         result.GetTagCount(),
		ContributorCount: result.GetContributorCount(),
		LastCommit:       result.GetLastCommit(),
	}, nil
}

func (s *GytServer) GetCloneURLs(ctx context.Context, req *pb.GetCloneURLsRequest) (*pb.CloneURLsResponse, error) {
	callerID, _ := auth.ExtractUserID(ctx)
	result, err := s.Repos.GetCloneURLs(ctx, callerID, req.GetOwner(), req.GetName())
	if err != nil {
		return nil, err
	}
	return &pb.CloneURLsResponse{
		SshUrl:  result.GetSshUrl(),
		HttpUrl: result.GetHttpUrl(),
		GitUrl:  result.GetGitUrl(),
	}, nil
}

func (s *GytServer) CompareBranches(ctx context.Context, req *pb.CompareBranchesRequest) (*pb.CompareResponse, error) {
	callerID, _ := auth.ExtractUserID(ctx)
	result, err := s.Repos.CompareBranches(ctx, callerID, req.GetOwner(), req.GetName(), req.GetBaseBranch(), req.GetHeadBranch())
	if err != nil {
		return nil, err
	}
	return compareToProto(result), nil
}

func (s *GytServer) CompareCommits(ctx context.Context, req *pb.CompareCommitsRequest) (*pb.CompareResponse, error) {
	callerID, _ := auth.ExtractUserID(ctx)
	result, err := s.Repos.CompareCommits(ctx, callerID, req.GetOwner(), req.GetName(), req.GetBaseSha(), req.GetHeadSha())
	if err != nil {
		return nil, err
	}
	return compareToProto(result), nil
}

func (s *GytServer) GetFileHistory(ctx context.Context, req *pb.GetFileHistoryRequest) (*pb.GetFileHistoryResponse, error) {
	callerID, _ := auth.ExtractUserID(ctx)
	result, err := s.Repos.GetFileHistory(ctx, callerID, req.GetOwner(), req.GetName(), req.GetPath(), req.Ref, req.GetLimit())
	if err != nil {
		return nil, err
	}
	resp := &pb.GetFileHistoryResponse{}
	for _, c := range result.GetCommits() {
		resp.Commits = append(resp.Commits, commitToProto(c))
	}
	return resp, nil
}

func (s *GytServer) SearchCommits(ctx context.Context, req *pb.SearchCommitsRequest) (*pb.ListCommitsResponse, error) {
	callerID, _ := auth.ExtractUserID(ctx)
	result, err := s.Repos.SearchCommits(ctx, callerID, req.GetOwner(), req.GetName(), req.GetQuery(), req.Author, req.Ref, req.GetLimit())
	if err != nil {
		return nil, err
	}
	resp := &pb.ListCommitsResponse{Page: result.GetPage(), PerPage: result.GetPerPage(), HasMore: result.GetHasMore()}
	for _, c := range result.GetCommits() {
		resp.Commits = append(resp.Commits, commitToProto(c))
	}
	return resp, nil
}

func (s *GytServer) CheckPath(ctx context.Context, req *pb.CheckPathRequest) (*pb.CheckPathResponse, error) {
	callerID, _ := auth.ExtractUserID(ctx)
	result, err := s.Repos.CheckPath(ctx, callerID, req.GetOwner(), req.GetName(), req.GetPath(), req.Ref)
	if err != nil {
		return nil, err
	}
	return &pb.CheckPathResponse{
		Exists: result.GetExists(),
		IsDir:  result.GetIsDir(),
		IsFile: result.GetIsFile(),
		Size:   result.Size,
	}, nil
}

// Collaborators

func (s *GytServer) ListCollaborators(ctx context.Context, req *pb.ListCollaboratorsRequest) (*pb.ListCollaboratorsResponse, error) {
	callerID, _ := auth.ExtractUserID(ctx)
	collabs, err := s.Repos.ListCollaborators(ctx, callerID, req.GetOwner(), req.GetName())
	if err != nil {
		return nil, err
	}
	resp := &pb.ListCollaboratorsResponse{}
	for _, c := range collabs {
		resp.Collaborators = append(resp.Collaborators, &pb.CollaboratorResponse{
			Username:    c.User.Username,
			AccessLevel: c.AccessLevel,
		})
	}
	return resp, nil
}

func (s *GytServer) AddCollaborator(ctx context.Context, req *pb.AddCollaboratorRequest) (*emptypb.Empty, error) {
	callerID, err := auth.ExtractUserID(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.Repos.AddCollaborator(ctx, callerID, req.GetOwner(), req.GetName(), req.GetUsername(), req.GetAccessLevel()); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

func (s *GytServer) RemoveCollaborator(ctx context.Context, req *pb.RemoveCollaboratorRequest) (*emptypb.Empty, error) {
	callerID, err := auth.ExtractUserID(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.Repos.RemoveCollaborator(ctx, callerID, req.GetOwner(), req.GetName(), req.GetUsername()); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

func (s *GytServer) UpdateCollaborator(ctx context.Context, req *pb.UpdateCollaboratorRequest) (*emptypb.Empty, error) {
	callerID, err := auth.ExtractUserID(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.Repos.UpdateCollaborator(ctx, callerID, req.GetOwner(), req.GetName(), req.GetUsername(), req.GetAccessLevel()); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

// ─── Organizations ────────────────────────────────────────────────────────────

func (s *GytServer) CreateOrganization(ctx context.Context, req *pb.CreateOrgRequest) (*pb.OrganizationResponse, error) {
	callerID, err := auth.ExtractUserID(ctx)
	if err != nil {
		return nil, err
	}
	org, err := s.Orgs.CreateOrganization(ctx, callerID, req.GetName(), req.GetDisplayName(), req.GetDescription())
	if err != nil {
		return nil, err
	}
	return orgToProto(org, s.Orgs.CountOrgMembers(org.ID), s.Orgs.CountOrgRepos(org.ID)), nil
}

func (s *GytServer) GetOrganization(ctx context.Context, req *pb.GetOrgRequest) (*pb.OrganizationResponse, error) {
	org, err := s.Orgs.GetOrganization(ctx, req.GetName())
	if err != nil {
		return nil, err
	}
	return orgToProto(org, s.Orgs.CountOrgMembers(org.ID), s.Orgs.CountOrgRepos(org.ID)), nil
}

func (s *GytServer) ListOrganizations(ctx context.Context, req *pb.ListOrgsRequest) (*pb.ListOrgsResponse, error) {
	orgs, total, err := s.Orgs.ListOrganizations(ctx, int(req.GetPage()), int(req.GetPerPage()))
	if err != nil {
		return nil, err
	}
	resp := &pb.ListOrgsResponse{Total: int32(total)}
	for i := range orgs {
		resp.Organizations = append(resp.Organizations, orgToProto(&orgs[i], s.Orgs.CountOrgMembers(orgs[i].ID), s.Orgs.CountOrgRepos(orgs[i].ID)))
	}
	return resp, nil
}

func (s *GytServer) ListUserOrganizations(ctx context.Context, req *pb.ListUserOrgsRequest) (*pb.ListOrgsResponse, error) {
	orgs, err := s.Orgs.ListUserOrganizations(ctx, req.GetUsername())
	if err != nil {
		return nil, err
	}
	resp := &pb.ListOrgsResponse{Total: int32(len(orgs))}
	for i := range orgs {
		resp.Organizations = append(resp.Organizations, orgToProto(&orgs[i], s.Orgs.CountOrgMembers(orgs[i].ID), s.Orgs.CountOrgRepos(orgs[i].ID)))
	}
	return resp, nil
}

func (s *GytServer) UpdateOrganization(ctx context.Context, req *pb.UpdateOrgRequest) (*pb.OrganizationResponse, error) {
	callerID, err := auth.ExtractUserID(ctx)
	if err != nil {
		return nil, err
	}
	org, err := s.Orgs.UpdateOrganization(ctx, callerID, req.GetName(), req.DisplayName, req.Description, req.AvatarUrl)
	if err != nil {
		return nil, err
	}
	return orgToProto(org, s.Orgs.CountOrgMembers(org.ID), s.Orgs.CountOrgRepos(org.ID)), nil
}

func (s *GytServer) DeleteOrganization(ctx context.Context, req *pb.DeleteOrgRequest) (*emptypb.Empty, error) {
	callerID, err := auth.ExtractUserID(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.Orgs.DeleteOrganization(ctx, callerID, req.GetName()); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

func (s *GytServer) ListOrgMembers(ctx context.Context, req *pb.ListOrgMembersRequest) (*pb.ListOrgMembersResponse, error) {
	members, err := s.Orgs.ListOrgMembers(ctx, req.GetOrgName())
	if err != nil {
		return nil, err
	}
	resp := &pb.ListOrgMembersResponse{}
	for i := range members {
		resp.Members = append(resp.Members, &pb.OrgMemberResponse{
			User:     userToProto(&members[i].User),
			Role:     string(members[i].Role),
			JoinedAt: timestamppb.New(members[i].CreatedAt),
		})
	}
	return resp, nil
}

func (s *GytServer) AddOrgMember(ctx context.Context, req *pb.AddOrgMemberRequest) (*pb.OrgMemberResponse, error) {
	callerID, err := auth.ExtractUserID(ctx)
	if err != nil {
		return nil, err
	}
	m, err := s.Orgs.AddOrgMember(ctx, callerID, req.GetOrgName(), req.GetUsername(), req.GetRole())
	if err != nil {
		return nil, err
	}
	return &pb.OrgMemberResponse{
		User:     userToProto(&m.User),
		Role:     string(m.Role),
		JoinedAt: timestamppb.New(m.CreatedAt),
	}, nil
}

func (s *GytServer) UpdateOrgMember(ctx context.Context, req *pb.UpdateOrgMemberRequest) (*pb.OrgMemberResponse, error) {
	callerID, err := auth.ExtractUserID(ctx)
	if err != nil {
		return nil, err
	}
	m, err := s.Orgs.UpdateOrgMember(ctx, callerID, req.GetOrgName(), req.GetUsername(), req.GetRole())
	if err != nil {
		return nil, err
	}
	return &pb.OrgMemberResponse{
		User:     userToProto(&m.User),
		Role:     string(m.Role),
		JoinedAt: timestamppb.New(m.CreatedAt),
	}, nil
}

func (s *GytServer) RemoveOrgMember(ctx context.Context, req *pb.RemoveOrgMemberRequest) (*emptypb.Empty, error) {
	callerID, err := auth.ExtractUserID(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.Orgs.RemoveOrgMember(ctx, callerID, req.GetOrgName(), req.GetUsername()); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

func (s *GytServer) GetOrgMembership(ctx context.Context, req *pb.GetOrgMembershipRequest) (*pb.OrgMemberResponse, error) {
	m, err := s.Orgs.GetOrgMembership(ctx, req.GetOrgName(), req.GetUsername())
	if err != nil {
		return nil, err
	}
	return &pb.OrgMemberResponse{
		User:     userToProto(&m.User),
		Role:     string(m.Role),
		JoinedAt: timestamppb.New(m.CreatedAt),
	}, nil
}
