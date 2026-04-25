package gql

import (
	"encoding/base64"
	"fmt"

	"github.com/Gyt-project/backend-api/pkg/gql/model"
	pb "github.com/Gyt-project/backend-api/pkg/grpc"
	"github.com/Gyt-project/backend-api/pkg/models"
)

// ─── Proto → Model mappers ────────────────────────────────────────────────────

func pbUserToModel(u *pb.UserResponse) *model.User {
	if u == nil {
		return nil
	}
	return &model.User{
		UUID:        u.GetUuid(),
		Username:    u.GetUsername(),
		Email:       u.GetEmail(),
		DisplayName: u.GetDisplayName(),
		Bio:         u.GetBio(),
		AvatarURL:   u.GetAvatarUrl(),
		IsAdmin:     u.GetIsAdmin(),
		CreatedAt:   u.GetCreatedAt().AsTime(),
	}
}

func pbAuthToModel(a *pb.AuthResponse) *model.AuthResponse {
	if a == nil {
		return nil
	}
	return &model.AuthResponse{
		AccessToken:  a.GetAccessToken(),
		RefreshToken: a.GetRefreshToken(),
		ExpiresIn:    int(a.GetExpiresIn()),
		User:         pbUserToModel(a.GetUser()),
	}
}

func pbSSHKeyToModel(k *pb.SSHKeyResponse) *model.SSHKey {
	if k == nil {
		return nil
	}
	return &model.SSHKey{
		ID:        fmt.Sprintf("%d", k.GetId()),
		Name:      k.GetName(),
		PublicKey: k.GetPublicKey(),
		CreatedAt: k.GetCreatedAt().AsTime(),
	}
}

func pbRepoToModel(r *pb.RepositoryResponse) *model.Repository {
	if r == nil {
		return nil
	}
	return &model.Repository{
		UUID:          r.GetUuid(),
		Name:          r.GetName(),
		Description:   r.GetDescription(),
		DefaultBranch: r.GetDefaultBranch(),
		IsPrivate:     r.GetIsPrivate(),
		IsFork:        r.GetIsFork(),
		OwnerType:     r.GetOwnerType(),
		OwnerName:     r.GetOwnerName(),
		Stars:         int(r.GetStars()),
		Forks:         int(r.GetForks()),
		CreatedAt:     r.GetCreatedAt().AsTime(),
		UpdatedAt:     r.GetUpdatedAt().AsTime(),
	}
}

func pbAuthorToModel(a *pb.AuthorResponse) *model.Author {
	if a == nil {
		return nil
	}
	return &model.Author{
		Name:  a.GetName(),
		Email: a.GetEmail(),
		When:  a.GetWhen().AsTime(),
	}
}

func pbCommitToModel(c *pb.CommitResponse) *model.Commit {
	if c == nil {
		return nil
	}
	return &model.Commit{
		Sha:        c.GetSha(),
		Author:     pbAuthorToModel(c.GetAuthor()),
		Committer:  pbAuthorToModel(c.GetCommitter()),
		Message:    c.GetMessage(),
		ParentShas: c.GetParentShas(),
	}
}

func pbTagToModel(t *pb.TagResponse) *model.Tag {
	if t == nil {
		return nil
	}
	tag := &model.Tag{
		Name:      t.GetName(),
		FullName:  t.GetFullName(),
		CommitSha: t.GetCommitSha(),
	}
	if t.Message != nil {
		msg := t.GetMessage()
		tag.Message = &msg
	}
	return tag
}

func pbBranchToModel(b *pb.BranchResponse) *model.Branch {
	if b == nil {
		return nil
	}
	return &model.Branch{
		Name:      b.GetName(),
		FullName:  b.GetFullName(),
		CommitSha: b.GetCommitSha(),
	}
}

func pbTreeEntryToModel(e *pb.TreeEntryResponse) *model.TreeEntry {
	if e == nil {
		return nil
	}
	return &model.TreeEntry{
		Name:        e.GetName(),
		Path:        e.GetPath(),
		Mode:        e.GetMode(),
		Size:        int(e.GetSize()),
		IsDir:       e.GetIsDir(),
		IsSubmodule: e.GetIsSubmodule(),
	}
}

func pbFileDiffToModel(f *pb.FileDiffResponse) *model.FileDiff {
	if f == nil {
		return nil
	}
	fd := &model.FileDiff{
		Path:      f.GetPath(),
		Additions: int(f.GetAdditions()),
		Deletions: int(f.GetDeletions()),
		Status:    f.GetStatus(),
	}
	if f.OldPath != nil {
		op := f.GetOldPath()
		fd.OldPath = &op
	}
	return fd
}

func pbCollaboratorToModel(c *pb.CollaboratorResponse) *model.Collaborator {
	if c == nil {
		return nil
	}
	return &model.Collaborator{
		Username:    c.GetUsername(),
		AccessLevel: c.GetAccessLevel(),
	}
}

func pbOrgToModel(o *pb.OrganizationResponse) *model.Organization {
	if o == nil {
		return nil
	}
	return &model.Organization{
		UUID:          o.GetUuid(),
		Name:          o.GetName(),
		DisplayName:   o.GetDisplayName(),
		Description:   o.GetDescription(),
		AvatarURL:     o.GetAvatarUrl(),
		OwnerUsername: o.GetOwnerUsername(),
		MemberCount:   int(o.GetMemberCount()),
		RepoCount:     int(o.GetRepoCount()),
		CreatedAt:     o.GetCreatedAt().AsTime(),
	}
}

func pbOrgMemberToModel(m *pb.OrgMemberResponse) *model.OrgMember {
	if m == nil {
		return nil
	}
	return &model.OrgMember{
		User:     pbUserToModel(m.GetUser()),
		Role:     m.GetRole(),
		JoinedAt: m.GetJoinedAt().AsTime(),
	}
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func int32Ptr(v *int) *int32 {
	if v == nil {
		return nil
	}
	x := int32(*v)
	return &x
}

func blobToBase64(b []byte) string {
	return base64.StdEncoding.EncodeToString(b)
}

// ─── Helpers déplacés depuis schema.resolvers.go ──────────────────────────────

func pbListReposToModel(resp *pb.ListReposResponse) *model.ListReposResponse {
	out := &model.ListReposResponse{
		Total:        int(resp.GetTotal()),
		Page:         int(resp.GetPage()),
		PerPage:      int(resp.GetPerPage()),
		Repositories: make([]*model.Repository, 0),
	}
	for _, r := range resp.GetRepositories() {
		out.Repositories = append(out.Repositories, pbRepoToModel(r))
	}
	return out
}

func pbListCommitsToModel(resp *pb.ListCommitsResponse) *model.ListCommitsResponse {
	out := &model.ListCommitsResponse{
		Page:    int(resp.GetPage()),
		PerPage: int(resp.GetPerPage()),
		HasMore: resp.GetHasMore(),
		Commits: make([]*model.Commit, 0),
	}
	for _, c := range resp.GetCommits() {
		out.Commits = append(out.Commits, pbCommitToModel(c))
	}
	return out
}

func pbCompareToModel(resp *pb.CompareResponse) *model.CompareResponse {
	out := &model.CompareResponse{
		TotalAdditions: int(resp.GetTotalAdditions()),
		TotalDeletions: int(resp.GetTotalDeletions()),
		FilesChanged:   int(resp.GetFilesChanged()),
		CommitsAhead:   int(resp.GetCommitsAhead()),
		Patch:          resp.GetPatch(),
		Commits:        make([]*model.Commit, 0),
		Files:          make([]*model.FileDiff, 0),
	}
	for _, c := range resp.GetCommits() {
		out.Commits = append(out.Commits, pbCommitToModel(c))
	}
	for _, f := range resp.GetFiles() {
		out.Files = append(out.Files, pbFileDiffToModel(f))
	}
	return out
}

// ─── Nouveaux mappers proto → model ──────────────────────────────────────────

func pbLabelToModel(l *pb.LabelResponse) *model.Label {
	if l == nil {
		return nil
	}
	return &model.Label{
		ID:          fmt.Sprintf("%d", l.GetId()),
		Name:        l.GetName(),
		Color:       l.GetColor(),
		Description: l.GetDescription(),
	}
}

func pbIssueToModel(i *pb.IssueResponse) *model.Issue {
	if i == nil {
		return nil
	}
	out := &model.Issue{
		ID:           fmt.Sprintf("%d", i.GetId()),
		Number:       int(i.GetNumber()),
		Title:        i.GetTitle(),
		Body:         i.GetBody(),
		State:        i.GetState(),
		Author:       pbUserToModel(i.GetAuthor()),
		CommentCount: int(i.GetCommentCount()),
		CreatedAt:    i.GetCreatedAt().AsTime(),
		UpdatedAt:    i.GetUpdatedAt().AsTime(),
		Assignees:    make([]*model.User, 0),
		Labels:       make([]*model.Label, 0),
	}
	for _, u := range i.GetAssignees() {
		out.Assignees = append(out.Assignees, pbUserToModel(u))
	}
	for _, l := range i.GetLabels() {
		out.Labels = append(out.Labels, pbLabelToModel(l))
	}
	if i.ClosedAt != nil {
		t := i.GetClosedAt().AsTime()
		out.ClosedAt = &t
	}
	return out
}

func pbIssueCommentToModel(c *pb.IssueCommentResponse) *model.IssueComment {
	if c == nil {
		return nil
	}
	return &model.IssueComment{
		ID:        fmt.Sprintf("%d", c.GetId()),
		Body:      c.GetBody(),
		Author:    pbUserToModel(c.GetAuthor()),
		CreatedAt: c.GetCreatedAt().AsTime(),
		UpdatedAt: c.GetUpdatedAt().AsTime(),
	}
}

func pbPRToModel(pr *pb.PullRequestResponse) *model.PullRequest {
	if pr == nil {
		return nil
	}
	out := &model.PullRequest{
		ID:           fmt.Sprintf("%d", pr.GetId()),
		Number:       int(pr.GetNumber()),
		Title:        pr.GetTitle(),
		Body:         pr.GetBody(),
		State:        pr.GetState(),
		HeadBranch:   pr.GetHeadBranch(),
		BaseBranch:   pr.GetBaseBranch(),
		HeadSha:      pr.GetHeadSha(),
		Author:       pbUserToModel(pr.GetAuthor()),
		Mergeable:    pr.GetMergeable(),
		Merged:       pr.GetMerged(),
		CommentCount: int(pr.GetCommentCount()),
		Commits:      int(pr.GetCommits()),
		Additions:    int(pr.GetAdditions()),
		Deletions:    int(pr.GetDeletions()),
		ChangedFiles: int(pr.GetChangedFiles()),
		CreatedAt:    pr.GetCreatedAt().AsTime(),
		UpdatedAt:    pr.GetUpdatedAt().AsTime(),
		Assignees:    make([]*model.User, 0),
		Labels:       make([]*model.Label, 0),
	}
	for _, u := range pr.GetAssignees() {
		out.Assignees = append(out.Assignees, pbUserToModel(u))
	}
	for _, l := range pr.GetLabels() {
		out.Labels = append(out.Labels, pbLabelToModel(l))
	}
	if pr.MergedAt != nil {
		t := pr.GetMergedAt().AsTime()
		out.MergedAt = &t
	}
	return out
}

func pbPRCommentToModel(c *pb.PRCommentResponse) *model.PRComment {
	if c == nil {
		return nil
	}
	out := &model.PRComment{
		ID:        fmt.Sprintf("%d", c.GetId()),
		Body:      c.GetBody(),
		Author:    pbUserToModel(c.GetAuthor()),
		CreatedAt: c.GetCreatedAt().AsTime(),
		UpdatedAt: c.GetUpdatedAt().AsTime(),
	}
	if c.Path != nil {
		p := c.GetPath()
		out.Path = &p
	}
	if c.Line != nil {
		l := int(c.GetLine())
		out.Line = &l
	}
	return out
}

func pbPRReviewToModel(r *pb.PRReviewResponse) *model.PRReview {
	if r == nil {
		return nil
	}
	return &model.PRReview{
		ID:          fmt.Sprintf("%d", r.GetId()),
		Reviewer:    pbUserToModel(r.GetReviewer()),
		State:       r.GetState(),
		Body:        r.GetBody(),
		SubmittedAt: r.GetSubmittedAt().AsTime(),
	}
}

// ─── ORM → Model mappers ─────────────────────────────────────────────────────

func prReviewToModel(r *models.PRReview) *model.PRReview {
	if r == nil {
		return nil
	}
	out := &model.PRReview{
		ID:            fmt.Sprintf("%d", r.ID),
		State:         r.State,
		Body:          r.Body,
		SubmittedAt:   r.CreatedAt,
		Dismissed:     r.Dismissed,
		DismissedAt:   r.DismissedAt,
		DismissReason: r.DismissReason,
	}
	if r.Reviewer.ID != 0 {
		out.Reviewer = dbUserToModel(&r.Reviewer)
	}
	return out
}

func branchProtectionToModel(rule *models.BranchProtection) *model.BranchProtection {
	if rule == nil {
		return nil
	}
	return &model.BranchProtection{
		ID:                  fmt.Sprintf("%d", rule.ID),
		Pattern:             rule.Pattern,
		RequirePullRequest:  rule.RequirePullRequest,
		RequiredApprovals:   rule.RequiredApprovals,
		DismissStaleReviews: rule.DismissStaleReviews,
		BlockForcePush:      rule.BlockForcePush,
		CreatedAt:           rule.CreatedAt,
		UpdatedAt:           rule.UpdatedAt,
	}
}

func dbUserToModel(u *models.User) *model.User {
	if u == nil {
		return nil
	}
	return &model.User{
		UUID:        u.UUID.String(),
		Username:    u.Username,
		Email:       u.Email,
		DisplayName: u.DisplayName,
		Bio:         u.Bio,
		AvatarURL:   u.AvatarURL,
		IsAdmin:     u.IsAdmin,
		CreatedAt:   u.CreatedAt,
	}
}

func pbWebhookToModel(w *pb.WebhookResponse) *model.Webhook {
	if w == nil {
		return nil
	}
	return &model.Webhook{
		ID:          fmt.Sprintf("%d", w.GetId()),
		URL:         w.GetUrl(),
		Events:      w.GetEvents(),
		Active:      w.GetActive(),
		ContentType: w.GetContentType(),
		CreatedAt:   w.GetCreatedAt().AsTime(),
		UpdatedAt:   w.GetUpdatedAt().AsTime(),
	}
}
