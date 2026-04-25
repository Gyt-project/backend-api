package server

import (
	"fmt"

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
		Patch:          r.GetPatch(),
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

// ─── Nouveaux mappers models → proto ─────────────────────────────────────────

func labelToProto(l *models.Label) *pb.LabelResponse {
	if l == nil {
		return nil
	}
	return &pb.LabelResponse{
		Id:          uint64(l.ID),
		Name:        l.Name,
		Color:       l.Color,
		Description: l.Description,
	}
}

func userToProtoSlice(users []models.User) []*pb.UserResponse {
	var out []*pb.UserResponse
	for i := range users {
		out = append(out, userToProto(&users[i]))
	}
	return out
}

func labelToProtoSlice(labels []models.Label) []*pb.LabelResponse {
	var out []*pb.LabelResponse
	for i := range labels {
		out = append(out, labelToProto(&labels[i]))
	}
	return out
}

func issueToProto(i *models.Issue) *pb.IssueResponse {
	if i == nil {
		return nil
	}
	resp := &pb.IssueResponse{
		Id:           uint64(i.ID),
		Number:       int32(i.Number),
		Title:        i.Title,
		Body:         i.Body,
		State:        i.State,
		Author:       userToProto(&i.Author),
		Assignees:    userToProtoSlice(i.Assignees),
		Labels:       labelToProtoSlice(i.Labels),
		CommentCount: int32(len(i.Comments)),
		CreatedAt:    timestamppb.New(i.CreatedAt),
		UpdatedAt:    timestamppb.New(i.UpdatedAt),
	}
	if i.ClosedAt != nil {
		resp.ClosedAt = timestamppb.New(*i.ClosedAt)
	}
	return resp
}

func issueCommentToProto(c *models.IssueComment) *pb.IssueCommentResponse {
	if c == nil {
		return nil
	}
	return &pb.IssueCommentResponse{
		Id:        uint64(c.ID),
		Body:      c.Body,
		Author:    userToProto(&c.Author),
		CreatedAt: timestamppb.New(c.CreatedAt),
		UpdatedAt: timestamppb.New(c.UpdatedAt),
	}
}

func prToProto(pr *models.PullRequest) *pb.PullRequestResponse {
	if pr == nil {
		return nil
	}
	resp := &pb.PullRequestResponse{
		Id:           uint64(pr.ID),
		Number:       int32(pr.Number),
		Title:        pr.Title,
		Body:         pr.Body,
		State:        pr.State,
		HeadBranch:   pr.HeadBranch,
		BaseBranch:   pr.BaseBranch,
		HeadSha:      pr.HeadSHA,
		Author:       userToProto(&pr.Author),
		Assignees:    userToProtoSlice(pr.Assignees),
		Labels:       labelToProtoSlice(pr.Labels),
		Mergeable:    pr.Mergeable,
		Merged:       pr.Merged,
		CommentCount: int32(len(pr.Comments)),
		CreatedAt:    timestamppb.New(pr.CreatedAt),
		UpdatedAt:    timestamppb.New(pr.UpdatedAt),
	}
	if pr.MergedAt != nil {
		resp.MergedAt = timestamppb.New(*pr.MergedAt)
	}
	return resp
}

func prCommentToProto(c *models.PRComment) *pb.PRCommentResponse {
	if c == nil {
		return nil
	}
	resp := &pb.PRCommentResponse{
		Id:        uint64(c.ID),
		Body:      c.Body,
		Author:    userToProto(&c.Author),
		CreatedAt: timestamppb.New(c.CreatedAt),
		UpdatedAt: timestamppb.New(c.UpdatedAt),
	}
	if c.Path != nil {
		resp.Path = c.Path
	}
	if c.Line != nil {
		l := int32(*c.Line)
		resp.Line = &l
	}
	return resp
}

func prReviewToProto(r *models.PRReview) *pb.PRReviewResponse {
	if r == nil {
		return nil
	}
	return &pb.PRReviewResponse{
		Id:          uint64(r.ID),
		Reviewer:    userToProto(&r.Reviewer),
		State:       r.State,
		Body:        r.Body,
		SubmittedAt: timestamppb.New(r.UpdatedAt),
	}
}

func reviewRequestToProto(r *models.ReviewRequest) *pb.ReviewRequestResponse {
	if r == nil {
		return nil
	}
	return &pb.ReviewRequestResponse{
		Id:          uint64(r.ID),
		Reviewer:    userToProto(&r.Reviewer),
		RequestedBy: userToProto(&r.RequestedBy),
		CreatedAt:   timestamppb.New(r.CreatedAt),
	}
}

func webhookToProto(w *models.Webhook) *pb.WebhookResponse {
	if w == nil {
		return nil
	}
	return &pb.WebhookResponse{
		Id:          uint64(w.ID),
		Url:         w.URL,
		Events:      service.DecodeEvents(w.Events),
		Active:      w.Active,
		ContentType: w.ContentType,
		CreatedAt:   timestamppb.New(w.CreatedAt),
		UpdatedAt:   timestamppb.New(w.UpdatedAt),
	}
}

// stargazerUserToProto convertit un slice de users en réponse stargazers.
func stargazersToProto(users []models.User, total int64) *pb.ListStargazersResponse {
	resp := &pb.ListStargazersResponse{Total: int32(total)}
	for i := range users {
		resp.Users = append(resp.Users, userToProto(&users[i]))
	}
	return resp
}

// fmtUint64 convertit uint64 en string (pour les IDs).
func fmtUint64(id uint64) string {
	return fmt.Sprintf("%d", id)
}
