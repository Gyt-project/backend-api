package server

import (
	"github.com/Gyt-project/backend-api/internal/service"
	pb "github.com/Gyt-project/backend-api/pkg/grpc"
	"github.com/Gyt-project/backend-api/pkg/models"
	ssgrpc "github.com/Gyt-project/soft-serve/pkg/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ─── Mappers ─────────────────────────────────────────────────────────────────

func userToProto(u *models.User) *pb.UserResponse {
	if u == nil {
		return nil
	}
	return &pb.UserResponse{
		Uuid:        u.UUID.String(),
		Username:    u.Username,
		Email:       u.Email,
		DisplayName: u.DisplayName,
		Bio:         u.Bio,
		AvatarUrl:   u.AvatarURL,
		IsAdmin:     u.IsAdmin,
		CreatedAt:   timestamppb.New(u.CreatedAt),
	}
}

func repoToProto(r *models.Repository, ownerName string) *pb.RepositoryResponse {
	if r == nil {
		return nil
	}
	return &pb.RepositoryResponse{
		Uuid:          r.UUID.String(),
		Name:          r.Name,
		Description:   r.Description,
		DefaultBranch: r.DefaultBranch,
		IsPrivate:     r.IsPrivate,
		IsFork:        r.IsFork,
		OwnerType:     string(r.OwnerType),
		OwnerName:     ownerName,
		Stars:         int32(r.Stars),
		Forks:         int32(r.Forks),
		CreatedAt:     timestamppb.New(r.CreatedAt),
		UpdatedAt:     timestamppb.New(r.UpdatedAt),
	}
}

func reposToListResponse(repos []models.Repository, total int64, page, perPage int32, rs *service.RepoService) *pb.ListReposResponse {
	resp := &pb.ListReposResponse{
		Total:   int32(total),
		Page:    page,
		PerPage: perPage,
	}
	for i := range repos {
		resp.Repositories = append(resp.Repositories, repoToProto(&repos[i], rs.OwnerName(&repos[i])))
	}
	return resp
}

func orgToProto(o *models.Organization, memberCount, repoCount int32) *pb.OrganizationResponse {
	if o == nil {
		return nil
	}
	ownerUsername := o.Owner.Username
	return &pb.OrganizationResponse{
		Uuid:          o.UUID.String(),
		Name:          o.Name,
		DisplayName:   o.DisplayName,
		Description:   o.Description,
		AvatarUrl:     o.AvatarURL,
		OwnerUsername: ownerUsername,
		MemberCount:   memberCount,
		RepoCount:     repoCount,
		CreatedAt:     timestamppb.New(o.CreatedAt),
	}
}

func commitToProto(c *ssgrpc.Commit) *pb.CommitResponse {
	if c == nil {
		return nil
	}
	resp := &pb.CommitResponse{
		Sha:        c.GetSha(),
		Message:    c.GetMessage(),
		ParentShas: c.GetParentShas(),
	}
	if a := c.GetAuthor(); a != nil {
		resp.Author = &pb.AuthorResponse{
			Name:  a.GetName(),
			Email: a.GetEmail(),
			When:  a.GetWhen(),
		}
	}
	if cm := c.GetCommitter(); cm != nil {
		resp.Committer = &pb.AuthorResponse{
			Name:  cm.GetName(),
			Email: cm.GetEmail(),
			When:  cm.GetWhen(),
		}
	}
	return resp
}

func compareToProto(r *ssgrpc.CompareResponse) *pb.CompareResponse {
	if r == nil {
		return nil
	}
	resp := &pb.CompareResponse{
		TotalAdditions: r.GetTotalAdditions(),
		TotalDeletions: r.GetTotalDeletions(),
		FilesChanged:   r.GetFilesChanged(),
		CommitsAhead:   r.GetCommitsAhead(),
	}
	for _, c := range r.GetCommits() {
		resp.Commits = append(resp.Commits, commitToProto(c))
	}
	for _, f := range r.GetFiles() {
		resp.Files = append(resp.Files, &pb.FileDiffResponse{
			Path:      f.GetPath(),
			Additions: f.GetAdditions(),
			Deletions: f.GetDeletions(),
			Status:    f.GetStatus(),
			OldPath:   f.OldPath,
		})
	}
	return resp
}
