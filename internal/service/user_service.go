package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Gyt-project/backend-api/internal/auth"
	"github.com/Gyt-project/backend-api/internal/gitClient"
	"github.com/Gyt-project/backend-api/internal/orm"
	"github.com/Gyt-project/backend-api/pkg/models"
	ssgrpc "github.com/Gyt-project/soft-serve/pkg/grpc"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

// UserService gère la logique métier des utilisateurs
type UserService struct{}

// hashPassword hashe un mot de passe avec bcrypt
func hashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(bytes), err
}

// checkPassword compare un mot de passe à son hash
func checkPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// Register crée un utilisateur dans la DB et dans le git server
func (s *UserService) Register(ctx context.Context, username, email, password, displayName string) (*models.User, string, string, error) {
	// Validation basique
	username = strings.ToLower(strings.TrimSpace(username))
	if username == "" || email == "" || password == "" {
		return nil, "", "", status.Error(codes.InvalidArgument, "username, email and password are required")
	}
	if err := validateIdentifier("username", username); err != nil {
		return nil, "", "", err
	}
	// Interdire le préfixe réservé aux orgs
	if strings.HasPrefix(username, "org-") {
		return nil, "", "", status.Error(codes.InvalidArgument, "username cannot start with 'org-'")
	}

	hashedPwd, err := hashPassword(password)
	if err != nil {
		return nil, "", "", status.Error(codes.Internal, "failed to hash password")
	}

	// Créer le user dans soft-serve
	gitUser, err := gitClient.GitClient.CreateUser(ctx, &ssgrpc.CreateUserRequest{
		Username:   username,
		Admin:      false,
		Password:   &password,
		PublicKeys: []string{},
	})
	if err != nil {
		return nil, "", "", status.Errorf(codes.Internal, "failed to create git user: %v", err)
	}

	// Persister en DB
	user := &models.User{
		Username:    username,
		Email:       email,
		Password:    hashedPwd,
		DisplayName: displayName,
		GitUsername: username,
		GitID:       fmt.Sprintf("%d", gitUser.GetId()),
	}
	if err := orm.DB.Create(user).Error; err != nil {
		// Rollback git user
		_ = gitClient.GitClient.DeleteUser(ctx, &ssgrpc.DeleteUserRequest{Username: username})
		return nil, "", "", status.Errorf(codes.AlreadyExists, "user already exists: %v", err)
	}

	// Générer les tokens JWT
	accessToken, err := auth.GenerateAccessToken(user.UUID.String(), user.ID, user.IsAdmin)
	if err != nil {
		return nil, "", "", status.Error(codes.Internal, "failed to generate access token")
	}
	refreshToken, err := auth.GenerateRefreshToken(user.UUID.String())
	if err != nil {
		return nil, "", "", status.Error(codes.Internal, "failed to generate refresh token")
	}

	return user, accessToken, refreshToken, nil
}

// Login authentifie un utilisateur par username ou email
func (s *UserService) Login(ctx context.Context, login, password string) (*models.User, string, string, error) {
	var user models.User
	result := orm.DB.Where("username = ? OR email = ?", login, login).First(&user)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil, "", "", status.Error(codes.NotFound, "invalid credentials")
	}
	if result.Error != nil {
		return nil, "", "", status.Error(codes.Internal, "database error")
	}
	if !checkPassword(user.Password, password) {
		return nil, "", "", status.Error(codes.Unauthenticated, "invalid credentials")
	}

	accessToken, err := auth.GenerateAccessToken(user.UUID.String(), user.ID, user.IsAdmin)
	if err != nil {
		return nil, "", "", status.Error(codes.Internal, "failed to generate access token")
	}
	refreshToken, err := auth.GenerateRefreshToken(user.UUID.String())
	if err != nil {
		return nil, "", "", status.Error(codes.Internal, "failed to generate refresh token")
	}

	return &user, accessToken, refreshToken, nil
}

// RefreshToken renouvelle les tokens à partir d'un refresh token
func (s *UserService) RefreshToken(ctx context.Context, refreshToken string) (*models.User, string, string, error) {
	userUUID, err := auth.ParseRefreshToken(refreshToken)
	if err != nil {
		return nil, "", "", status.Error(codes.Unauthenticated, "invalid refresh token")
	}

	var user models.User
	if err := orm.DB.Where("uuid = ?", userUUID).First(&user).Error; err != nil {
		return nil, "", "", status.Error(codes.NotFound, "user not found")
	}

	newAccess, err := auth.GenerateAccessToken(user.UUID.String(), user.ID, user.IsAdmin)
	if err != nil {
		return nil, "", "", status.Error(codes.Internal, "failed to generate access token")
	}
	newRefresh, err := auth.GenerateRefreshToken(user.UUID.String())
	if err != nil {
		return nil, "", "", status.Error(codes.Internal, "failed to generate refresh token")
	}

	return &user, newAccess, newRefresh, nil
}

// GetByUsername retourne un utilisateur par son username
func (s *UserService) GetByUsername(ctx context.Context, username string) (*models.User, error) {
	var user models.User
	if err := orm.DB.Where("username = ?", username).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, status.Error(codes.NotFound, "user not found")
		}
		return nil, status.Error(codes.Internal, "database error")
	}
	return &user, nil
}

// GetByUUID retourne un utilisateur par son UUID
func (s *UserService) GetByUUID(ctx context.Context, uuid string) (*models.User, error) {
	var user models.User
	if err := orm.DB.Where("uuid = ?", uuid).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, status.Error(codes.NotFound, "user not found")
		}
		return nil, status.Error(codes.Internal, "database error")
	}
	return &user, nil
}

// GetByID retourne un utilisateur par son ID DB
func (s *UserService) GetByID(ctx context.Context, id uint) (*models.User, error) {
	var user models.User
	if err := orm.DB.First(&user, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, status.Error(codes.NotFound, "user not found")
		}
		return nil, status.Error(codes.Internal, "database error")
	}
	return &user, nil
}

// UpdateUser met à jour les champs d'un utilisateur
func (s *UserService) UpdateUser(ctx context.Context, userID uint, displayName, bio, avatarURL, email, newPassword *string) (*models.User, error) {
	var user models.User
	if err := orm.DB.First(&user, userID).Error; err != nil {
		return nil, status.Error(codes.NotFound, "user not found")
	}

	updates := map[string]interface{}{}
	if displayName != nil {
		updates["display_name"] = *displayName
	}
	if bio != nil {
		updates["bio"] = *bio
	}
	if avatarURL != nil {
		updates["avatar_url"] = *avatarURL
	}
	if email != nil {
		updates["email"] = *email
	}
	if newPassword != nil {
		hashed, err := hashPassword(*newPassword)
		if err != nil {
			return nil, status.Error(codes.Internal, "failed to hash password")
		}
		updates["password"] = hashed
		// Mettre à jour aussi le mot de passe côté git
		_, _ = gitClient.GitClient.UpdateUser(ctx, &ssgrpc.UpdateUserRequest{
			Username: user.Username,
			Password: newPassword,
		})
	}

	if err := orm.DB.Model(&user).Updates(updates).Error; err != nil {
		return nil, status.Error(codes.Internal, "failed to update user")
	}

	return &user, nil
}

// DeleteUser supprime un utilisateur (soft-delete DB + suppression git)
func (s *UserService) DeleteUser(ctx context.Context, username string) error {
	var user models.User
	if err := orm.DB.Where("username = ?", username).First(&user).Error; err != nil {
		return status.Error(codes.NotFound, "user not found")
	}

	// Supprimer de soft-serve
	if err := gitClient.GitClient.DeleteUser(ctx, &ssgrpc.DeleteUserRequest{Username: user.GitUsername}); err != nil {
		return status.Errorf(codes.Internal, "failed to delete git user: %v", err)
	}

	// Soft-delete en DB
	if err := orm.DB.Delete(&user).Error; err != nil {
		return status.Error(codes.Internal, "failed to delete user from database")
	}
	return nil
}

// ListUsers retourne une liste paginée d'utilisateurs
func (s *UserService) ListUsers(ctx context.Context, page, perPage int) ([]models.User, int64, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 100 {
		perPage = 30
	}
	offset := (page - 1) * perPage

	var users []models.User
	var total int64
	orm.DB.Model(&models.User{}).Count(&total)
	if err := orm.DB.Offset(offset).Limit(perPage).Find(&users).Error; err != nil {
		return nil, 0, status.Error(codes.Internal, "database error")
	}
	return users, total, nil
}

// AddSSHKey ajoute une clé SSH à un utilisateur
func (s *UserService) AddSSHKey(ctx context.Context, userID uint, name, publicKey string) (*models.SSHKey, error) {
	var user models.User
	if err := orm.DB.First(&user, userID).Error; err != nil {
		return nil, status.Error(codes.NotFound, "user not found")
	}

	// Ajouter côté soft-serve
	if err := gitClient.GitClient.AddPublicKey(ctx, &ssgrpc.AddPublicKeyRequest{
		Username:  user.GitUsername,
		PublicKey: publicKey,
	}); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to add public key to git server: %v", err)
	}

	key := &models.SSHKey{
		UserID:    userID,
		Name:      name,
		PublicKey: publicKey,
	}
	if err := orm.DB.Create(key).Error; err != nil {
		// Rollback
		_ = gitClient.GitClient.RemovePublicKey(ctx, &ssgrpc.RemovePublicKeyRequest{
			Username:  user.GitUsername,
			PublicKey: publicKey,
		})
		return nil, status.Error(codes.AlreadyExists, "SSH key already exists")
	}
	return key, nil
}

// DeleteSSHKey supprime une clé SSH
func (s *UserService) DeleteSSHKey(ctx context.Context, userID uint, keyID uint) error {
	var key models.SSHKey
	if err := orm.DB.Where("id = ? AND user_id = ?", keyID, userID).First(&key).Error; err != nil {
		return status.Error(codes.NotFound, "SSH key not found")
	}

	var user models.User
	if err := orm.DB.First(&user, userID).Error; err != nil {
		return status.Error(codes.NotFound, "user not found")
	}

	// Supprimer côté soft-serve
	if err := gitClient.GitClient.RemovePublicKey(ctx, &ssgrpc.RemovePublicKeyRequest{
		Username:  user.GitUsername,
		PublicKey: key.PublicKey,
	}); err != nil {
		return status.Errorf(codes.Internal, "failed to remove public key from git server: %v", err)
	}

	return orm.DB.Delete(&key).Error
}

// ListSSHKeys retourne les clés SSH d'un utilisateur
func (s *UserService) ListSSHKeys(ctx context.Context, username string) ([]models.SSHKey, error) {
	var user models.User
	if err := orm.DB.Where("username = ?", username).First(&user).Error; err != nil {
		return nil, status.Error(codes.NotFound, "user not found")
	}

	var keys []models.SSHKey
	if err := orm.DB.Where("user_id = ?", user.ID).Find(&keys).Error; err != nil {
		return nil, status.Error(codes.Internal, "database error")
	}
	return keys, nil
}
