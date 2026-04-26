// Package server contient les handlers gRPC pour le service GYT.
// NOTE: Ce fichier implémente GytServiceServer généré par protoc.
// Comme le proto n'est pas encore compilé, on déclare l'interface manuellement.
// Une fois `protoc` exécuté, remplacer par l'interface générée.
package server

import (
	"context"
	"strconv"

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
	Users      *service.UserService
	Repos      *service.RepoService
	Orgs       *service.OrgService
	Stars      *service.StarService
	Labels     *service.LabelService
	Issues     *service.IssueService
	PRs        *service.PRService
	BranchProt *service.BranchProtectionService
	Webhooks   *service.WebhookService
	Search     *service.SearchService
}

func NewGytServer() *GytServer {
	return &GytServer{
		Users:      &service.UserService{},
		Repos:      &service.RepoService{},
		Orgs:       &service.OrgService{},
		Stars:      &service.StarService{},
		Labels:     &service.LabelService{},
		Issues:     &service.IssueService{},
		PRs:        &service.PRService{},
		BranchProt: &service.BranchProtectionService{},
		Webhooks:   &service.WebhookService{},
		Search:     &service.SearchService{},
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
		// User was deleted — treat the token as invalid so the client clears its session
		return nil, status.Error(codes.Unauthenticated, "user not found, please log in again")
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

func (s *GytServer) CreateBranch(ctx context.Context, req *pb.CreateBranchRequest) (*pb.BranchResponse, error) {
	callerID, err := auth.ExtractUserID(ctx)
	if err != nil {
		return nil, err
	}
	b, err := s.Repos.CreateBranch(ctx, callerID, req.GetOwner(), req.GetName(), req.GetBranchName(), req.GetSource())
	if err != nil {
		return nil, err
	}
	return &pb.BranchResponse{
		Name:      b.GetName(),
		FullName:  b.GetFullName(),
		CommitSha: b.GetCommitSha(),
	}, nil
}

func (s *GytServer) DeleteBranch(ctx context.Context, req *pb.DeleteBranchRequest) (*emptypb.Empty, error) {
	callerID, err := auth.ExtractUserID(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.Repos.DeleteBranch(ctx, callerID, req.GetOwner(), req.GetName(), req.GetBranchName()); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
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

// ─── Stars ────────────────────────────────────────────────────────────────────

func (s *GytServer) StarRepository(ctx context.Context, req *pb.StarRepoRequest) (*emptypb.Empty, error) {
	callerID, err := auth.ExtractUserID(ctx)
	if err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, s.Stars.StarRepository(ctx, callerID, req.GetOwner(), req.GetName())
}

func (s *GytServer) UnstarRepository(ctx context.Context, req *pb.UnstarRepoRequest) (*emptypb.Empty, error) {
	callerID, err := auth.ExtractUserID(ctx)
	if err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, s.Stars.UnstarRepository(ctx, callerID, req.GetOwner(), req.GetName())
}

func (s *GytServer) CheckStar(ctx context.Context, req *pb.CheckStarRequest) (*pb.CheckStarResponse, error) {
	callerID, _ := auth.ExtractUserID(ctx)
	starred, err := s.Stars.CheckStar(ctx, callerID, req.GetOwner(), req.GetName())
	if err != nil {
		return nil, err
	}
	return &pb.CheckStarResponse{Starred: starred}, nil
}

func (s *GytServer) ListStargazers(ctx context.Context, req *pb.ListStargazersRequest) (*pb.ListStargazersResponse, error) {
	users, total, err := s.Stars.ListStargazers(ctx, req.GetOwner(), req.GetName(), int(req.GetPage()), int(req.GetPerPage()))
	if err != nil {
		return nil, err
	}
	return stargazersToProto(users, total), nil
}

func (s *GytServer) ListStarredRepositories(ctx context.Context, req *pb.ListStarredReposRequest) (*pb.ListReposResponse, error) {
	repos, total, err := s.Stars.ListStarredRepositories(ctx, req.GetUsername(), int(req.GetPage()), int(req.GetPerPage()))
	if err != nil {
		return nil, err
	}
	return reposToListResponse(repos, total, req.GetPage(), req.GetPerPage(), s.Repos), nil
}

// ─── Labels ───────────────────────────────────────────────────────────────────

func (s *GytServer) CreateLabel(ctx context.Context, req *pb.CreateLabelRequest) (*pb.LabelResponse, error) {
	callerID, err := auth.ExtractUserID(ctx)
	if err != nil {
		return nil, err
	}
	label, err := s.Labels.CreateLabel(ctx, callerID, req.GetOwner(), req.GetRepo(), req.GetName(), req.GetColor(), req.Description)
	if err != nil {
		return nil, err
	}
	return labelToProto(label), nil
}

func (s *GytServer) GetLabel(ctx context.Context, req *pb.GetLabelRequest) (*pb.LabelResponse, error) {
	label, err := s.Labels.GetLabel(ctx, req.GetOwner(), req.GetRepo(), req.GetName())
	if err != nil {
		return nil, err
	}
	return labelToProto(label), nil
}

func (s *GytServer) ListLabels(ctx context.Context, req *pb.ListLabelsRequest) (*pb.ListLabelsResponse, error) {
	labels, err := s.Labels.ListLabels(ctx, req.GetOwner(), req.GetRepo())
	if err != nil {
		return nil, err
	}
	resp := &pb.ListLabelsResponse{}
	for i := range labels {
		resp.Labels = append(resp.Labels, labelToProto(&labels[i]))
	}
	return resp, nil
}

func (s *GytServer) UpdateLabel(ctx context.Context, req *pb.UpdateLabelRequest) (*pb.LabelResponse, error) {
	callerID, err := auth.ExtractUserID(ctx)
	if err != nil {
		return nil, err
	}
	label, err := s.Labels.UpdateLabel(ctx, callerID, req.GetOwner(), req.GetRepo(), req.GetName(), req.NewName, req.Color, req.Description)
	if err != nil {
		return nil, err
	}
	return labelToProto(label), nil
}

func (s *GytServer) DeleteLabel(ctx context.Context, req *pb.DeleteLabelRequest) (*emptypb.Empty, error) {
	callerID, err := auth.ExtractUserID(ctx)
	if err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, s.Labels.DeleteLabel(ctx, callerID, req.GetOwner(), req.GetRepo(), req.GetName())
}

// ─── Issues ───────────────────────────────────────────────────────────────────

func (s *GytServer) CreateIssue(ctx context.Context, req *pb.CreateIssueRequest) (*pb.IssueResponse, error) {
	callerID, err := auth.ExtractUserID(ctx)
	if err != nil {
		return nil, err
	}
	issue, err := s.Issues.CreateIssue(ctx, callerID, req.GetOwner(), req.GetRepo(), req.GetTitle(), req.Body, req.GetAssignees(), req.GetLabels())
	if err != nil {
		return nil, err
	}
	return issueToProto(issue), nil
}

func (s *GytServer) GetIssue(ctx context.Context, req *pb.GetIssueRequest) (*pb.IssueResponse, error) {
	issue, err := s.Issues.GetIssue(ctx, req.GetOwner(), req.GetRepo(), int(req.GetNumber()))
	if err != nil {
		return nil, err
	}
	return issueToProto(issue), nil
}

func (s *GytServer) ListIssues(ctx context.Context, req *pb.ListIssuesRequest) (*pb.ListIssuesResponse, error) {
	issues, total, err := s.Issues.ListIssues(ctx, req.GetOwner(), req.Repo, req.State, req.Label, req.Assignee, req.Author, int(req.GetPage()), int(req.GetPerPage()))
	if err != nil {
		return nil, err
	}
	resp := &pb.ListIssuesResponse{Total: int32(total), Page: req.GetPage(), PerPage: req.GetPerPage()}
	for i := range issues {
		resp.Issues = append(resp.Issues, issueToProto(&issues[i]))
	}
	return resp, nil
}

func (s *GytServer) UpdateIssue(ctx context.Context, req *pb.UpdateIssueRequest) (*pb.IssueResponse, error) {
	callerID, err := auth.ExtractUserID(ctx)
	if err != nil {
		return nil, err
	}
	issue, err := s.Issues.UpdateIssue(ctx, callerID, req.GetOwner(), req.GetRepo(), int(req.GetNumber()), req.Title, req.Body)
	if err != nil {
		return nil, err
	}
	return issueToProto(issue), nil
}

func (s *GytServer) CloseIssue(ctx context.Context, req *pb.CloseIssueRequest) (*pb.IssueResponse, error) {
	callerID, err := auth.ExtractUserID(ctx)
	if err != nil {
		return nil, err
	}
	issue, err := s.Issues.CloseIssue(ctx, callerID, req.GetOwner(), req.GetRepo(), int(req.GetNumber()))
	if err != nil {
		return nil, err
	}
	return issueToProto(issue), nil
}

func (s *GytServer) ReopenIssue(ctx context.Context, req *pb.ReopenIssueRequest) (*pb.IssueResponse, error) {
	callerID, err := auth.ExtractUserID(ctx)
	if err != nil {
		return nil, err
	}
	issue, err := s.Issues.ReopenIssue(ctx, callerID, req.GetOwner(), req.GetRepo(), int(req.GetNumber()))
	if err != nil {
		return nil, err
	}
	return issueToProto(issue), nil
}

func (s *GytServer) AddIssueLabel(ctx context.Context, req *pb.AddIssueLabelRequest) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, s.Issues.AddLabel(ctx, req.GetOwner(), req.GetRepo(), int(req.GetNumber()), req.GetLabelName())
}

func (s *GytServer) RemoveIssueLabel(ctx context.Context, req *pb.RemoveIssueLabelRequest) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, s.Issues.RemoveLabel(ctx, req.GetOwner(), req.GetRepo(), int(req.GetNumber()), req.GetLabelName())
}

func (s *GytServer) AddIssueAssignee(ctx context.Context, req *pb.AddIssueAssigneeRequest) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, s.Issues.AddAssignee(ctx, req.GetOwner(), req.GetRepo(), int(req.GetNumber()), req.GetUsername())
}

func (s *GytServer) RemoveIssueAssignee(ctx context.Context, req *pb.RemoveIssueAssigneeRequest) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, s.Issues.RemoveAssignee(ctx, req.GetOwner(), req.GetRepo(), int(req.GetNumber()), req.GetUsername())
}

func (s *GytServer) CreateIssueComment(ctx context.Context, req *pb.CreateIssueCommentRequest) (*pb.IssueCommentResponse, error) {
	callerID, err := auth.ExtractUserID(ctx)
	if err != nil {
		return nil, err
	}
	comment, err := s.Issues.CreateComment(ctx, callerID, req.GetOwner(), req.GetRepo(), int(req.GetNumber()), req.GetBody())
	if err != nil {
		return nil, err
	}
	return issueCommentToProto(comment), nil
}

func (s *GytServer) ListIssueComments(ctx context.Context, req *pb.ListIssueCommentsRequest) (*pb.ListIssueCommentsResponse, error) {
	comments, err := s.Issues.ListComments(ctx, req.GetOwner(), req.GetRepo(), int(req.GetNumber()))
	if err != nil {
		return nil, err
	}
	resp := &pb.ListIssueCommentsResponse{}
	for i := range comments {
		resp.Comments = append(resp.Comments, issueCommentToProto(&comments[i]))
	}
	return resp, nil
}

func (s *GytServer) UpdateIssueComment(ctx context.Context, req *pb.UpdateIssueCommentRequest) (*pb.IssueCommentResponse, error) {
	callerID, err := auth.ExtractUserID(ctx)
	if err != nil {
		return nil, err
	}
	comment, err := s.Issues.UpdateComment(ctx, callerID, req.GetOwner(), req.GetRepo(), uint(req.GetCommentId()), req.GetBody())
	if err != nil {
		return nil, err
	}
	return issueCommentToProto(comment), nil
}

func (s *GytServer) DeleteIssueComment(ctx context.Context, req *pb.DeleteIssueCommentRequest) (*emptypb.Empty, error) {
	callerID, err := auth.ExtractUserID(ctx)
	if err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, s.Issues.DeleteComment(ctx, callerID, req.GetOwner(), req.GetRepo(), uint(req.GetCommentId()))
}

// ─── Pull Requests ────────────────────────────────────────────────────────────

func (s *GytServer) CreatePullRequest(ctx context.Context, req *pb.CreatePRRequest) (*pb.PullRequestResponse, error) {
	callerID, err := auth.ExtractUserID(ctx)
	if err != nil {
		return nil, err
	}
	pr, err := s.PRs.CreatePullRequest(ctx, callerID, req.GetOwner(), req.GetRepo(), req.GetTitle(), req.GetHeadBranch(), req.GetBaseBranch(), req.Body, req.GetAssignees(), req.GetLabels())
	if err != nil {
		return nil, err
	}
	return prToProto(pr), nil
}

func (s *GytServer) GetPullRequest(ctx context.Context, req *pb.GetPRRequest) (*pb.PullRequestResponse, error) {
	pr, err := s.PRs.GetPullRequestFull(ctx, req.GetOwner(), req.GetRepo(), int(req.GetNumber()))
	if err != nil {
		return nil, err
	}
	return prToProto(pr), nil
}

func (s *GytServer) ListPullRequests(ctx context.Context, req *pb.ListPRsRequest) (*pb.ListPRsResponse, error) {
	prs, total, err := s.PRs.ListPullRequests(ctx, req.GetOwner(), req.Repo, req.State, req.Author, req.Assignee, req.Label, req.Base, int(req.GetPage()), int(req.GetPerPage()))
	if err != nil {
		return nil, err
	}
	resp := &pb.ListPRsResponse{Total: int32(total), Page: req.GetPage(), PerPage: req.GetPerPage()}
	for i := range prs {
		resp.PullRequests = append(resp.PullRequests, prToProto(&prs[i]))
	}
	return resp, nil
}

func (s *GytServer) UpdatePullRequest(ctx context.Context, req *pb.UpdatePRRequest) (*pb.PullRequestResponse, error) {
	callerID, err := auth.ExtractUserID(ctx)
	if err != nil {
		return nil, err
	}
	pr, err := s.PRs.UpdatePullRequest(ctx, callerID, req.GetOwner(), req.GetRepo(), int(req.GetNumber()), req.Title, req.Body, req.Base)
	if err != nil {
		return nil, err
	}
	return prToProto(pr), nil
}

func (s *GytServer) MergePullRequest(ctx context.Context, req *pb.MergePRRequest) (*pb.MergePRResponse, error) {
	callerID, err := auth.ExtractUserID(ctx)
	if err != nil {
		return nil, err
	}
	merged, sha, msg, err := s.PRs.MergePullRequest(ctx, callerID, req.GetOwner(), req.GetRepo(), int(req.GetNumber()), req.MergeMethod, req.CommitTitle, req.CommitMessage)
	if err != nil {
		return nil, err
	}
	return &pb.MergePRResponse{Merged: merged, Sha: sha, Message: msg}, nil
}

func (s *GytServer) ClosePullRequest(ctx context.Context, req *pb.ClosePRRequest) (*pb.PullRequestResponse, error) {
	callerID, err := auth.ExtractUserID(ctx)
	if err != nil {
		return nil, err
	}
	pr, err := s.PRs.ClosePullRequest(ctx, callerID, req.GetOwner(), req.GetRepo(), int(req.GetNumber()))
	if err != nil {
		return nil, err
	}
	return prToProto(pr), nil
}

func (s *GytServer) ReopenPullRequest(ctx context.Context, req *pb.ReopenPRRequest) (*pb.PullRequestResponse, error) {
	callerID, err := auth.ExtractUserID(ctx)
	if err != nil {
		return nil, err
	}
	pr, err := s.PRs.ReopenPullRequest(ctx, callerID, req.GetOwner(), req.GetRepo(), int(req.GetNumber()))
	if err != nil {
		return nil, err
	}
	return prToProto(pr), nil
}

func (s *GytServer) GetPullRequestDiff(ctx context.Context, req *pb.GetPRDiffRequest) (*pb.CompareResponse, error) {
	_, pr, err := s.PRs.GetPullRequestBase(ctx, req.GetOwner(), req.GetRepo(), int(req.GetNumber()))
	if err != nil {
		return nil, err
	}
	callerID, _ := auth.ExtractUserID(ctx)
	result, err := s.Repos.CompareBranches(ctx, callerID, req.GetOwner(), req.GetRepo(), pr.BaseBranch, pr.HeadBranch)
	if err != nil {
		return nil, err
	}
	return compareToProto(result), nil
}

func (s *GytServer) CreatePRComment(ctx context.Context, req *pb.CreatePRCommentRequest) (*pb.PRCommentResponse, error) {
	callerID, err := auth.ExtractUserID(ctx)
	if err != nil {
		return nil, err
	}
	var line *int
	if req.Line != nil {
		v := int(req.GetLine())
		line = &v
	}
	var commitSHA *string
	if req.CommitSha != nil {
		v := req.GetCommitSha()
		commitSHA = &v
	}
	comment, err := s.PRs.CreateComment(ctx, callerID, req.GetOwner(), req.GetRepo(), int(req.GetNumber()), req.GetBody(), req.Path, line, commitSHA)
	if err != nil {
		return nil, err
	}
	return prCommentToProto(comment), nil
}

func (s *GytServer) ListPRComments(ctx context.Context, req *pb.ListPRCommentsRequest) (*pb.ListPRCommentsResponse, error) {
	comments, err := s.PRs.ListComments(ctx, req.GetOwner(), req.GetRepo(), int(req.GetNumber()))
	if err != nil {
		return nil, err
	}
	resp := &pb.ListPRCommentsResponse{}
	for i := range comments {
		resp.Comments = append(resp.Comments, prCommentToProto(&comments[i]))
	}
	return resp, nil
}

func (s *GytServer) UpdatePRComment(ctx context.Context, req *pb.UpdatePRCommentRequest) (*pb.PRCommentResponse, error) {
	callerID, err := auth.ExtractUserID(ctx)
	if err != nil {
		return nil, err
	}
	comment, err := s.PRs.UpdateComment(ctx, callerID, req.GetOwner(), req.GetRepo(), uint(req.GetCommentId()), req.GetBody())
	if err != nil {
		return nil, err
	}
	return prCommentToProto(comment), nil
}

func (s *GytServer) DeletePRComment(ctx context.Context, req *pb.DeletePRCommentRequest) (*emptypb.Empty, error) {
	callerID, err := auth.ExtractUserID(ctx)
	if err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, s.PRs.DeleteComment(ctx, callerID, req.GetOwner(), req.GetRepo(), uint(req.GetCommentId()))
}

func (s *GytServer) CreatePRReview(ctx context.Context, req *pb.CreatePRReviewRequest) (*pb.PRReviewResponse, error) {
	callerID, err := auth.ExtractUserID(ctx)
	if err != nil {
		return nil, err
	}
	review, err := s.PRs.CreateReview(ctx, callerID, req.GetOwner(), req.GetRepo(), int(req.GetNumber()), req.GetState(), req.GetBody())
	if err != nil {
		return nil, err
	}
	return prReviewToProto(review), nil
}

func (s *GytServer) ListPRReviews(ctx context.Context, req *pb.ListPRReviewsRequest) (*pb.ListPRReviewsResponse, error) {
	reviews, err := s.PRs.ListReviews(ctx, req.GetOwner(), req.GetRepo(), int(req.GetNumber()))
	if err != nil {
		return nil, err
	}
	resp := &pb.ListPRReviewsResponse{}
	for i := range reviews {
		resp.Reviews = append(resp.Reviews, prReviewToProto(&reviews[i]))
	}
	return resp, nil
}

func (s *GytServer) DismissReview(ctx context.Context, req *pb.DismissReviewRequest) (*pb.PRReviewResponse, error) {
	callerID, err := auth.ExtractUserID(ctx)
	if err != nil {
		return nil, err
	}
	review, err := s.PRs.DismissReview(ctx, callerID, req.GetOwner(), req.GetRepo(), strconv.FormatUint(req.GetReviewId(), 10), req.GetReason())
	if err != nil {
		return nil, err
	}
	return prReviewToProto(review), nil
}

func (s *GytServer) RequestReview(ctx context.Context, req *pb.RequestReviewRequest) (*emptypb.Empty, error) {
	callerID, err := auth.ExtractUserID(ctx)
	if err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, s.PRs.RequestReview(ctx, callerID, req.GetOwner(), req.GetRepo(), int(req.GetNumber()), req.GetUsername())
}

func (s *GytServer) RemoveReviewRequest(ctx context.Context, req *pb.RemoveReviewRequestRequest) (*emptypb.Empty, error) {
	callerID, err := auth.ExtractUserID(ctx)
	if err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, s.PRs.RemoveReviewRequest(ctx, callerID, req.GetOwner(), req.GetRepo(), int(req.GetNumber()), req.GetUsername())
}

func (s *GytServer) ListReviewRequests(ctx context.Context, req *pb.ListReviewRequestsRequest) (*pb.ListReviewRequestsResponse, error) {
	requests, err := s.PRs.ListReviewRequests(ctx, req.GetOwner(), req.GetRepo(), int(req.GetNumber()))
	if err != nil {
		return nil, err
	}
	resp := &pb.ListReviewRequestsResponse{}
	for i := range requests {
		resp.Requests = append(resp.Requests, reviewRequestToProto(&requests[i]))
	}
	return resp, nil
}

func (s *GytServer) AddPRLabel(ctx context.Context, req *pb.AddPRLabelRequest) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, s.PRs.AddLabel(ctx, req.GetOwner(), req.GetRepo(), int(req.GetNumber()), req.GetLabelName())
}

func (s *GytServer) RemovePRLabel(ctx context.Context, req *pb.RemovePRLabelRequest) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, s.PRs.RemoveLabel(ctx, req.GetOwner(), req.GetRepo(), int(req.GetNumber()), req.GetLabelName())
}

func (s *GytServer) AddPRAssignee(ctx context.Context, req *pb.AddPRAssigneeRequest) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, s.PRs.AddAssignee(ctx, req.GetOwner(), req.GetRepo(), int(req.GetNumber()), req.GetUsername())
}

func (s *GytServer) RemovePRAssignee(ctx context.Context, req *pb.RemovePRAssigneeRequest) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, s.PRs.RemoveAssignee(ctx, req.GetOwner(), req.GetRepo(), int(req.GetNumber()), req.GetUsername())
}

func (s *GytServer) GetPRMergeEligibility(ctx context.Context, req *pb.GetPRMergeEligibilityRequest) (*pb.PRMergeEligibilityResponse, error) {
	canMerge, reason, required, current, blockedByChanges, err := s.PRs.CheckMergeEligibility(ctx, req.GetOwner(), req.GetRepo(), int(req.GetNumber()))
	if err != nil {
		return nil, err
	}
	return &pb.PRMergeEligibilityResponse{
		CanMerge:                canMerge,
		Reason:                  reason,
		RequiredApprovals:       int32(required),
		CurrentApprovals:        int32(current),
		BlockedByChangesRequest: blockedByChanges,
	}, nil
}

func (s *GytServer) DismissStaleReviews(ctx context.Context, req *pb.DismissStaleReviewsRequest) (*emptypb.Empty, error) {
	callerID, err := auth.ExtractUserID(ctx)
	if err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, s.PRs.DismissStaleReviews(ctx, callerID, req.GetOwner(), req.GetRepo(), int(req.GetNumber()))
}

func (s *GytServer) HandleBranchPush(ctx context.Context, req *pb.BranchPushRequest) (*pb.BranchPushResponse, error) {
	numbers, err := s.PRs.HandleBranchPush(ctx, req.GetOwner(), req.GetRepo(), req.GetBranch())
	if err != nil {
		// Non-fatal: return empty response so the gateway can still publish the repo push event.
		return &pb.BranchPushResponse{}, nil
	}
	resp := &pb.BranchPushResponse{}
	for _, n := range numbers {
		resp.PrNumbers = append(resp.PrNumbers, int32(n))
	}
	return resp, nil
}

// ─── Branch Protection ───────────────────────────────────────────────────────

func (s *GytServer) CreateBranchProtection(ctx context.Context, req *pb.CreateBranchProtectionRequest) (*pb.BranchProtectionResponse, error) {
	callerID, err := auth.ExtractUserID(ctx)
	if err != nil {
		return nil, err
	}
	rule, err := s.BranchProt.Create(ctx, callerID, req.GetOwner(), req.GetRepo(), req.GetPattern(), req.GetRequirePullRequest(), int(req.GetRequiredApprovals()), req.GetDismissStaleReviews(), req.GetBlockForcePush())
	if err != nil {
		return nil, err
	}
	return branchProtectionToProto(rule), nil
}

func (s *GytServer) GetBranchProtection(ctx context.Context, req *pb.GetBranchProtectionRequest) (*pb.BranchProtectionResponse, error) {
	rule, err := s.BranchProt.Get(ctx, req.GetOwner(), req.GetRepo(), strconv.FormatUint(req.GetId(), 10))
	if err != nil {
		return nil, err
	}
	return branchProtectionToProto(rule), nil
}

func (s *GytServer) ListBranchProtections(ctx context.Context, req *pb.ListBranchProtectionsRequest) (*pb.ListBranchProtectionsResponse, error) {
	rules, err := s.BranchProt.List(ctx, req.GetOwner(), req.GetRepo())
	if err != nil {
		return nil, err
	}
	resp := &pb.ListBranchProtectionsResponse{}
	for i := range rules {
		resp.Rules = append(resp.Rules, branchProtectionToProto(&rules[i]))
	}
	return resp, nil
}

func (s *GytServer) UpdateBranchProtection(ctx context.Context, req *pb.UpdateBranchProtectionRequest) (*pb.BranchProtectionResponse, error) {
	callerID, err := auth.ExtractUserID(ctx)
	if err != nil {
		return nil, err
	}
	var pattern *string
	if req.Pattern != nil {
		v := req.GetPattern()
		pattern = &v
	}
	var requirePR *bool
	if req.RequirePullRequest != nil {
		v := req.GetRequirePullRequest()
		requirePR = &v
	}
	var requiredApprovals *int
	if req.RequiredApprovals != nil {
		v := int(req.GetRequiredApprovals())
		requiredApprovals = &v
	}
	var dismissStale *bool
	if req.DismissStaleReviews != nil {
		v := req.GetDismissStaleReviews()
		dismissStale = &v
	}
	var blockForcePush *bool
	if req.BlockForcePush != nil {
		v := req.GetBlockForcePush()
		blockForcePush = &v
	}
	rule, err := s.BranchProt.Update(ctx, callerID, req.GetOwner(), req.GetRepo(), strconv.FormatUint(req.GetId(), 10), pattern, requirePR, requiredApprovals, dismissStale, blockForcePush)
	if err != nil {
		return nil, err
	}
	return branchProtectionToProto(rule), nil
}

func (s *GytServer) DeleteBranchProtection(ctx context.Context, req *pb.DeleteBranchProtectionRequest) (*emptypb.Empty, error) {
	callerID, err := auth.ExtractUserID(ctx)
	if err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, s.BranchProt.Delete(ctx, callerID, req.GetOwner(), req.GetRepo(), strconv.FormatUint(req.GetId(), 10))
}

// ─── Webhooks ─────────────────────────────────────────────────────────────────

func (s *GytServer) CreateWebhook(ctx context.Context, req *pb.CreateWebhookRequest) (*pb.WebhookResponse, error) {
	wh, err := s.Webhooks.CreateWebhook(ctx, req.GetOwner(), req.Repo, req.GetUrl(), req.GetEvents(), req.Secret, req.Active, req.ContentType)
	if err != nil {
		return nil, err
	}
	return webhookToProto(wh), nil
}

func (s *GytServer) GetWebhook(ctx context.Context, req *pb.GetWebhookRequest) (*pb.WebhookResponse, error) {
	wh, err := s.Webhooks.GetWebhook(ctx, req.GetOwner(), req.Repo, uint(req.GetId()))
	if err != nil {
		return nil, err
	}
	return webhookToProto(wh), nil
}

func (s *GytServer) ListWebhooks(ctx context.Context, req *pb.ListWebhooksRequest) (*pb.ListWebhooksResponse, error) {
	whs, err := s.Webhooks.ListWebhooks(ctx, req.GetOwner(), req.Repo)
	if err != nil {
		return nil, err
	}
	resp := &pb.ListWebhooksResponse{}
	for i := range whs {
		resp.Webhooks = append(resp.Webhooks, webhookToProto(&whs[i]))
	}
	return resp, nil
}

func (s *GytServer) UpdateWebhook(ctx context.Context, req *pb.UpdateWebhookRequest) (*pb.WebhookResponse, error) {
	wh, err := s.Webhooks.UpdateWebhook(ctx, req.GetOwner(), req.Repo, uint(req.GetId()), req.Url, req.GetEvents(), req.Active, req.Secret, req.ContentType)
	if err != nil {
		return nil, err
	}
	return webhookToProto(wh), nil
}

func (s *GytServer) DeleteWebhook(ctx context.Context, req *pb.DeleteWebhookRequest) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, s.Webhooks.DeleteWebhook(ctx, req.GetOwner(), req.Repo, uint(req.GetId()))
}

func (s *GytServer) PingWebhook(ctx context.Context, req *pb.PingWebhookRequest) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, s.Webhooks.PingWebhook(ctx, req.GetOwner(), req.Repo, uint(req.GetId()))
}

// ─── Search ───────────────────────────────────────────────────────────────────

func (s *GytServer) SearchRepositories(ctx context.Context, req *pb.SearchReposRequest) (*pb.ListReposResponse, error) {
	repos, total, err := s.Search.SearchRepositories(ctx, req.GetQuery(), req.Language, req.Sort, req.Order, int(req.GetPage()), int(req.GetPerPage()))
	if err != nil {
		return nil, err
	}
	return reposToListResponse(repos, total, req.GetPage(), req.GetPerPage(), s.Repos), nil
}

func (s *GytServer) SearchUsers(ctx context.Context, req *pb.SearchUsersRequest) (*pb.ListUsersResponse, error) {
	users, total, err := s.Search.SearchUsers(ctx, req.GetQuery(), req.Sort, req.Order, int(req.GetPage()), int(req.GetPerPage()))
	if err != nil {
		return nil, err
	}
	resp := &pb.ListUsersResponse{Total: int32(total), Page: req.GetPage(), PerPage: req.GetPerPage()}
	for i := range users {
		resp.Users = append(resp.Users, userToProto(&users[i]))
	}
	return resp, nil
}

func (s *GytServer) SearchIssues(ctx context.Context, req *pb.SearchIssuesRequest) (*pb.ListIssuesResponse, error) {
	issues, total, err := s.Search.SearchIssues(ctx, req.GetQuery(), req.State, req.Type, req.Owner, req.Repo, req.Author, req.Label, req.Sort, req.Order, int(req.GetPage()), int(req.GetPerPage()))
	if err != nil {
		return nil, err
	}
	resp := &pb.ListIssuesResponse{Total: int32(total), Page: req.GetPage(), PerPage: req.GetPerPage()}
	for i := range issues {
		resp.Issues = append(resp.Issues, issueToProto(&issues[i]))
	}
	return resp, nil
}
