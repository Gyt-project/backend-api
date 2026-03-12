package gql

import (
	"encoding/base64"
	"fmt"

	"github.com/Gyt-project/backend-api/pkg/gql/model"
	pb "github.com/Gyt-project/backend-api/pkg/grpc"
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
