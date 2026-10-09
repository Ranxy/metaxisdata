package v1

import (
	"context"
	"fmt"
	"net/mail"
	"regexp"
	"slices"
	"strings"

	"connectrpc.com/connect"
	"github.com/pkg/errors"

	"golang.org/x/crypto/bcrypt"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/common/permission"
	"github.com/Ranxy/metaxisdata/backend/component/iam"
	"github.com/Ranxy/metaxisdata/backend/config"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/metaxisdata/backend/store"
	"github.com/Ranxy/metaxisdata/backend/utils"
)

// UserService implements the user service.
type UserService struct {
	v1connect.UnimplementedUserServiceHandler
	store   *store.Store
	iam     *iam.Manager
	profile *config.Profile
}

// NewUserService creates a new UserService.
func NewUserService(store *store.Store, iamManager *iam.Manager, profile *config.Profile) *UserService {
	return &UserService{
		store:   store,
		iam:     iamManager,
		profile: profile,
	}
}

// GetUser gets a user.
func (s *UserService) GetUser(ctx context.Context, request *connect.Request[v1pb.GetUserRequest]) (*connect.Response[v1pb.User], error) {
	userID, err := common.GetUserID(request.Msg.Name)
	var user *store.UserMessage
	// Report what was actually looked up: an email lookup that misses would
	// otherwise answer "user 0 not found".
	lookupName := request.Msg.Name
	if err != nil {
		email, err := common.GetUserEmail(request.Msg.Name)
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		lookupName = email
		u, err := s.store.GetUserByEmail(ctx, email)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to get user, error: %v", err))
		}
		user = u
	} else {
		u, err := s.store.GetUserByID(ctx, userID)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to get user, error: %v", err))
		}
		user = u
	}
	if user == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("user %q not found", lookupName))
	}
	return connect.NewResponse(convertToUser(user)), nil
}

// BatchGetUsers get users in batch.
func (s *UserService) BatchGetUsers(ctx context.Context, request *connect.Request[v1pb.BatchGetUsersRequest]) (*connect.Response[v1pb.BatchGetUsersResponse], error) {
	response := &v1pb.BatchGetUsersResponse{}
	for _, name := range request.Msg.Names {
		user, err := s.GetUser(ctx, connect.NewRequest(&v1pb.GetUserRequest{Name: name}))
		if err != nil {
			return nil, err
		}
		response.Users = append(response.Users, user.Msg)
	}
	return connect.NewResponse(response), nil
}

// GetCurrentUser gets the current authenticated user.
func (s *UserService) GetCurrentUser(ctx context.Context, _ *connect.Request[emptypb.Empty]) (*connect.Response[v1pb.User], error) {
	user, ok := GetUserFromContext(ctx)
	if !ok || user == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.Errorf("authenticated user not found"))
	}
	response := convertToUser(user)
	// The effective permissions are what the SPA gates navigation and actions
	// on; only GetCurrentUser populates them.
	permissions, err := s.iam.EffectivePermissions(ctx, user)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to resolve user permissions"))
	}
	response.Permissions = permissions
	return connect.NewResponse(response), nil
}

// ListUsers lists all users.
func (s *UserService) ListUsers(ctx context.Context, request *connect.Request[v1pb.ListUsersRequest]) (*connect.Response[v1pb.ListUsersResponse], error) {
	offset, err := parseLimitAndOffset(&pageSize{
		token:   request.Msg.PageToken,
		limit:   int(request.Msg.PageSize),
		maximum: 1000,
	})
	if err != nil {
		return nil, err
	}
	limitPlusOne := offset.limit + 1

	find := &store.FindUserMessage{
		Limit:       &limitPlusOne,
		Offset:      &offset.offset,
		ShowDeleted: request.Msg.ShowDeleted,
	}
	if err := parseListUserFilter(find, request.Msg.Filter); err != nil {
		return nil, err
	}

	users, err := s.store.ListUsers(ctx, find)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to list user, error: %v", err))
	}

	users, nextPageToken, err := paginate(users, offset)
	if err != nil {
		return nil, err
	}

	response := &v1pb.ListUsersResponse{
		NextPageToken: nextPageToken,
	}
	for _, user := range users {
		response.Users = append(response.Users, convertToUser(user))
	}
	return connect.NewResponse(response), nil
}

// CreateUser creates a user.
func (s *UserService) CreateUser(ctx context.Context, request *connect.Request[v1pb.CreateUserRequest]) (*connect.Response[v1pb.User], error) {
	if request.Msg.User == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("user must be set"))
	}
	if request.Msg.User.Email == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("email must be set"))
	}
	if request.Msg.User.Title == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("user title must be set"))
	}
	if err := validateUserTitle(request.Msg.User.Title); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	principalType, err := convertToPrincipalType(request.Msg.User.UserType)
	if err != nil {
		return nil, err
	}
	if request.Msg.User.UserType != v1pb.UserType_SERVICE_ACCOUNT && request.Msg.User.UserType != v1pb.UserType_END_USER {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("support user and service account only"))
	}
	if err := s.authorizeCreateUser(ctx, request.Msg.User.UserType); err != nil {
		return nil, err
	}

	if request.Msg.User.Phone != "" {
		if err := common.ValidatePhone(request.Msg.User.Phone); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid phone %q, error: %v", request.Msg.User.Phone, err))
		}
	}
	// Signup carries no language, and an unset one is left unset so that the SPA
	// can fill it in from the locale the user is actually reading.
	if request.Msg.User.Language != "" && !isSupportedLanguage(request.Msg.User.Language) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("unsupported language %q", request.Msg.User.Language))
	}

	if err := validateEmailWithDomains(ctx, s.store, request.Msg.User.Email, principalType == storepb.PrincipalType_SERVICE_ACCOUNT); err != nil {
		return nil, err
	}
	existingUser, err := s.store.GetUserByEmail(ctx, request.Msg.User.Email)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to find user by email, error: %v", err))
	}
	if existingUser != nil {
		return nil, connect.NewError(connect.CodeAlreadyExists, errors.Errorf("email %s is already existed", request.Msg.User.Email))
	}

	password := request.Msg.User.Password
	if request.Msg.User.UserType == v1pb.UserType_SERVICE_ACCOUNT {
		pwd, err := common.RandomString(20)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to generate access key for service account"))
		}
		password = fmt.Sprintf("%s%s", common.ServiceAccountAccessKeyPrefix, pwd)
	} else {
		if password != "" {
			if err := s.validatePassword(ctx, password); err != nil {
				return nil, err
			}
		} else {
			pwd, err := common.RandomString(20)
			if err != nil {
				return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to generate random password for service account"))
			}
			password = pwd
		}
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to generate password hash, error: %v", err))
	}
	userMessage := &store.UserMessage{
		Email:        request.Msg.User.Email,
		Name:         request.Msg.User.Title,
		Phone:        request.Msg.User.Phone,
		Type:         principalType,
		PasswordHash: string(passwordHash),
	}
	if request.Msg.User.Language != "" {
		userMessage.Profile = &storepb.UserProfile{Language: request.Msg.User.Language}
	}

	// A password account carries no identity provider binding.
	user, err := s.store.CreateUser(ctx, userMessage, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create user")
	}

	userResponse := convertToUser(user)
	if request.Msg.User.UserType == v1pb.UserType_SERVICE_ACCOUNT {
		userResponse.ServiceKey = password
	}
	return connect.NewResponse(userResponse), nil
}

// authorizeCreateUser distinguishes an admin creating a user from self-service
// signup.
//
// An admin may create any user. Everyone else may only sign up as an end user,
// and only while self-service signup is enabled. The very first end user is
// always allowed so that a fresh workspace can be bootstrapped.
func (s *UserService) authorizeCreateUser(ctx context.Context, userType v1pb.UserType) error {
	if caller, ok := GetUserFromContext(ctx); ok && caller != nil {
		isAdmin, err := s.iam.CheckPermission(ctx, permission.UsersCreate, caller)
		if err != nil {
			return connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to check permission"))
		}
		if isAdmin {
			return nil
		}
	}

	if userType != v1pb.UserType_END_USER {
		return connect.NewError(connect.CodePermissionDenied, errors.Errorf("only a workspace admin can create a %s", userType))
	}

	activeEndUserCount, err := s.store.CountUsers(ctx, storepb.PrincipalType_END_USER)
	if err != nil {
		return connect.NewError(connect.CodeInternal, errors.Errorf("failed to count users, error: %v", err))
	}
	if activeEndUserCount == 0 {
		return nil
	}

	setting, err := s.store.GetWorkspaceGeneralSetting(ctx)
	if err != nil {
		return connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to find workspace setting"))
	}
	if setting.DisallowSignup {
		return connect.NewError(connect.CodePermissionDenied, errors.Errorf("self-service signup is disallowed"))
	}
	return nil
}

func (s *UserService) validatePassword(ctx context.Context, password string) error {
	passwordRestriction, err := s.store.GetPasswordRestrictionSetting(ctx)
	if err != nil {
		return connect.NewError(connect.CodeInternal, errors.Errorf("failed to get password restriction with error: %v", err))
	}
	if len(password) < int(passwordRestriction.MinLength) {
		return connect.NewError(connect.CodeInvalidArgument, errors.Errorf("password length should no less than %v characters", passwordRestriction.MinLength))
	}
	if passwordRestriction.RequireNumber && !regexp.MustCompile("[0-9]+").MatchString(password) {
		return connect.NewError(connect.CodeInvalidArgument, errors.Errorf("password must contains at least 1 number"))
	}
	if passwordRestriction.RequireLetter && !regexp.MustCompile("[a-zA-Z]+").MatchString(password) {
		return connect.NewError(connect.CodeInvalidArgument, errors.Errorf("password must contains at least 1 lower case letter"))
	}
	if passwordRestriction.RequireUppercaseLetter && !regexp.MustCompile("[A-Z]+").MatchString(password) {
		return connect.NewError(connect.CodeInvalidArgument, errors.Errorf("password must contains at least 1 upper case letter"))
	}
	if passwordRestriction.RequireSpecialCharacter && !regexp.MustCompile(`[!@#$%^&*()_+\-=\[\]{};':"\\|,.<>\/?]+`).MatchString(password) {
		return connect.NewError(connect.CodeInvalidArgument, errors.Errorf("password must contains at least 1 special character"))
	}
	return nil
}

// UpdateUser updates a user.
func (s *UserService) UpdateUser(ctx context.Context, request *connect.Request[v1pb.UpdateUserRequest]) (*connect.Response[v1pb.User], error) {
	callerUser, ok := GetUserFromContext(ctx)
	if !ok || callerUser == nil {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.Errorf("failed to get caller user"))
	}
	if request.Msg.User == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("user must be set"))
	}
	if request.Msg.UpdateMask == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("update_mask must be set"))
	}

	userID, err := common.GetUserID(request.Msg.User.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to get user, error: %v", err))
	}
	if user == nil {
		if request.Msg.AllowMissing {
			// Creating a user through PATCH is still a user creation.
			if err := requirePermission(ctx, s.iam, permission.UsersCreate); err != nil {
				return nil, err
			}
			// AIP-134: when the resource is missing the mask selects the fields
			// used to create it, so the request body cannot smuggle in fields
			// the caller did not declare.
			return s.CreateUser(ctx, connect.NewRequest(&v1pb.CreateUserRequest{
				User: applyUpdateMaskToUser(request.Msg.User, request.Msg.UpdateMask.GetPaths()),
			}))
		}
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("user %d not found", userID))
	}
	if user.MemberDeleted {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("user %d has been deleted", userID))
	}

	// Updating another user, including rotating their credentials, requires
	// metaxisdata.users.update. A user may update themselves; per-field rules
	// (the email address) are applied below.
	isSelf := callerUser.ID == user.ID
	if !isSelf {
		if err := requirePermission(ctx, s.iam, permission.UsersUpdate); err != nil {
			return nil, err
		}
	}

	// A token restricted to a forced password reset authorizes exactly that
	// change and nothing else.
	if _, restricted := GetTokenRestrictionFromContext(ctx); restricted {
		if !isSelf || !slices.Equal(request.Msg.UpdateMask.GetPaths(), []string{"password"}) {
			return nil, connect.NewError(connect.CodePermissionDenied, errors.Errorf("access token restricted to a password reset can only change the user's own password"))
		}
	}

	var passwordPatch *string
	patch := &store.UpdateUserMessage{}
	for _, path := range request.Msg.UpdateMask.Paths {
		switch path {
		case "email":
			// The address is how the account is recognized outside the
			// workspace, so it is an administrative field: changing anyone's
			// email, including your own, requires metaxisdata.users.update.
			if err := requirePermission(ctx, s.iam, permission.UsersUpdate); err != nil {
				return nil, err
			}
			if err := validateEmailWithDomains(ctx, s.store, request.Msg.User.Email, user.Type == storepb.PrincipalType_SERVICE_ACCOUNT); err != nil {
				return nil, err
			}
			existedUser, err := s.store.GetUserByEmail(ctx, request.Msg.User.Email)
			if err != nil {
				return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to find user list, error: %v", err))
			}
			if existedUser != nil && existedUser.ID != user.ID {
				return nil, connect.NewError(connect.CodeAlreadyExists, errors.Errorf("email %s is already existed", request.Msg.User.Email))
			}
			patch.Email = &request.Msg.User.Email
		case "title":
			if err := validateUserTitle(request.Msg.User.Title); err != nil {
				return nil, connect.NewError(connect.CodeInvalidArgument, err)
			}
			patch.Name = &request.Msg.User.Title
		case "password":
			if user.Type != storepb.PrincipalType_END_USER {
				return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("password can be mutated for end users only"))
			}
			// Changing your own password requires proving you know it;
			// otherwise a stolen token is enough to take over the account.
			if isSelf {
				if request.Msg.CurrentPassword == "" {
					return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("current_password is required to change your own password"))
				}
				if err := bcrypt.CompareHashAndPassword([]byte(callerUser.PasswordHash), []byte(request.Msg.CurrentPassword)); err != nil {
					return nil, connect.NewError(connect.CodePermissionDenied, errors.Errorf("current_password is incorrect"))
				}
			}
			if err := s.validatePassword(ctx, request.Msg.User.Password); err != nil {
				return nil, err
			}
			passwordPatch = &request.Msg.User.Password
		case "service_key":
			if user.Type != storepb.PrincipalType_SERVICE_ACCOUNT {
				return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("service key can be mutated for service accounts only"))
			}
			val, err := common.RandomString(20)
			if err != nil {
				return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to generate access key for service account"))
			}
			password := fmt.Sprintf("%s%s", common.ServiceAccountAccessKeyPrefix, val)
			passwordPatch = &password
		case "phone":
			if request.Msg.User.Phone != "" {
				if err := common.ValidatePhone(request.Msg.User.Phone); err != nil {
					return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid phone number %q, error: %v", request.Msg.User.Phone, err))
				}
			}
			patch.Phone = &request.Msg.User.Phone
		case "language":
			// A preference the server cannot prompt in is refused here rather
			// than stored: it would only be answered in the default language.
			// Empty clears it back to the default.
			language := request.Msg.User.Language
			if language != "" && !isSupportedLanguage(language) {
				return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("unsupported language %q", language))
			}
			// A targeted write: the store touches one profile key, so the rest of
			// the profile is not rewritten from whatever this caller happened to
			// read.
			patch.Language = &language
		default:
		}
	}
	if passwordPatch != nil {
		if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(*passwordPatch)); err == nil {
			// return bad request if the passwords match
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("password cannot be the same"))
		}

		passwordHash, err := bcrypt.GenerateFromPassword([]byte((*passwordPatch)), bcrypt.DefaultCost)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to generate password hash, error: %v", err))
		}
		passwordHashStr := string(passwordHash)
		patch.PasswordHash = &passwordHashStr
	}

	user, err = s.store.UpdateUser(ctx, user, patch)
	if err != nil {
		return nil, errors.Wrap(err, "failed to update user")
	}

	userResponse := convertToUser(user)
	if request.Msg.User.UserType == v1pb.UserType_SERVICE_ACCOUNT && passwordPatch != nil {
		userResponse.ServiceKey = *passwordPatch
	}
	return connect.NewResponse(userResponse), nil
}

// applyUpdateMaskToUser keeps only the fields named by an update mask, so that a
// create through PATCH with allow_missing applies the mask instead of silently
// adopting the whole request body. `user_type` is a maskable path because it
// selects the kind of principal to create.
func applyUpdateMaskToUser(user *v1pb.User, paths []string) *v1pb.User {
	masked := &v1pb.User{}
	for _, path := range paths {
		switch path {
		case "email":
			masked.Email = user.Email
		case "title":
			masked.Title = user.Title
		case "password":
			masked.Password = user.Password
		case "phone":
			masked.Phone = user.Phone
		case "user_type":
			masked.UserType = user.UserType
		case "language":
			masked.Language = user.Language
		default:
		}
	}
	return masked
}

// DeleteUser deletes a user.
func (s *UserService) DeleteUser(ctx context.Context, request *connect.Request[v1pb.DeleteUserRequest]) (*connect.Response[emptypb.Empty], error) {
	if _, ok := GetUserFromContext(ctx); !ok {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.Errorf("failed to get caller user"))
	}

	// metaxisdata.users.delete is enforced by the ACL interceptor through the
	// method annotation.

	userID, err := common.GetUserID(request.Msg.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to get user, error: %v", err))
	}
	if user == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("user %d not found", userID))
	}
	if user.MemberDeleted {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("user %d has been deleted", userID))
	}

	// Check if there is still workspace admin if the current user is deleted.
	policy, err := s.store.GetWorkspaceIamPolicy(ctx)
	if err != nil {
		return nil, err
	}
	hasExtraWorkspaceAdmin, err := hasActiveWorkspaceAdmin(ctx, s.store, policy.Policy, user.ID)
	if err != nil {
		return nil, err
	}
	if !hasExtraWorkspaceAdmin {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("workspace must keep at least one active admin whose binding has no condition"))
	}

	if _, err := s.store.UpdateUser(ctx, user, &store.UpdateUserMessage{Delete: &deletePatch}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

// UndeleteUser undeletes a user.
func (s *UserService) UndeleteUser(ctx context.Context, request *connect.Request[v1pb.UndeleteUserRequest]) (*connect.Response[v1pb.User], error) {
	if _, ok := GetUserFromContext(ctx); !ok {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.Errorf("failed to get caller user"))
	}
	// metaxisdata.users.undelete is enforced by the ACL interceptor through the
	// method annotation.

	userID, err := common.GetUserID(request.Msg.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to get user, error: %v", err))
	}
	if user == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("user %d not found", userID))
	}
	if !user.MemberDeleted {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("user %d is already active", userID))
	}

	user, err = s.store.UpdateUser(ctx, user, &store.UpdateUserMessage{Delete: &undeletePatch})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(convertToUser(user)), nil
}

func convertToV1UserType(userType storepb.PrincipalType) v1pb.UserType {
	switch userType {
	case storepb.PrincipalType_END_USER:
		return v1pb.UserType_END_USER
	case storepb.PrincipalType_SYSTEM_BOT:
		return v1pb.UserType_SYSTEM_BOT
	case storepb.PrincipalType_SERVICE_ACCOUNT:
		return v1pb.UserType_SERVICE_ACCOUNT
	default:
		return v1pb.UserType_USER_TYPE_UNSPECIFIED
	}
}

func convertToUser(user *store.UserMessage) *v1pb.User {
	convertedUser := &v1pb.User{
		Name:     common.FormatUserUID(user.ID),
		State:    convertDeletedToState(user.MemberDeleted),
		Email:    user.Email,
		Phone:    user.Phone,
		Title:    user.Name,
		UserType: convertToV1UserType(user.Type),
		Language: user.Profile.GetLanguage(),
		Profile: &v1pb.UserProfile{
			LastLoginTime:          user.Profile.LastLoginTime,
			LastChangePasswordTime: user.Profile.LastChangePasswordTime,
		},
	}

	for _, group := range user.Groups {
		convertedUser.Groups = append(convertedUser.Groups, common.FormatGroupEmail(group))
	}

	return convertedUser
}

func convertToPrincipalType(userType v1pb.UserType) (storepb.PrincipalType, error) {
	var t storepb.PrincipalType
	switch userType {
	case v1pb.UserType_END_USER:
		t = storepb.PrincipalType_END_USER
	case v1pb.UserType_SYSTEM_BOT:
		t = storepb.PrincipalType_SYSTEM_BOT
	case v1pb.UserType_SERVICE_ACCOUNT:
		t = storepb.PrincipalType_SERVICE_ACCOUNT
	default:
		return t, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid user type %s", userType))
	}
	return t, nil
}

func validateEmailWithDomains(ctx context.Context, stores *store.Store, email string, isServiceAccount bool) error {
	setting, err := stores.GetWorkspaceGeneralSetting(ctx)
	if err != nil {
		return connect.NewError(connect.CodeInternal, errors.Errorf("failed to find workspace setting, error: %v", err))
	}

	var allowedDomains []string
	if setting.EnforceIdentityDomain {
		allowedDomains = setting.Domains
	}

	// Check if the email is valid.
	if err := validateEmail(email); err != nil {
		return connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid email: %v", err.Error()))
	}
	// Domain restrictions are not applied to service account.
	if isServiceAccount {
		return nil
	}
	// Enforce domain restrictions.
	if len(allowedDomains) > 0 {
		ok := false
		for _, v := range allowedDomains {
			if strings.HasSuffix(email, fmt.Sprintf("@%s", v)) {
				ok = true
				break
			}
		}
		if !ok {
			return connect.NewError(connect.CodeInvalidArgument, errors.Errorf("email %q does not belong to domains %v", email, allowedDomains))
		}
	}
	return nil
}

func validateEmail(email string) error {
	// 254 bytes is the RFC 5321 maximum for a forward-path address. The parser
	// itself accepts an arbitrarily long one, and CreateUser is reachable
	// without a credential.
	if len(email) > 254 {
		return errors.New("email is longer than 254 bytes")
	}
	if email != strings.ToLower(email) {
		return errors.New("email should be lowercase")
	}
	if _, err := mail.ParseAddress(email); err != nil {
		return err
	}
	return nil
}

// maxUserTitleBytes bounds a display name. An unbounded title lets an anonymous
// CreateUser park a request-sized value in the users table and in the ledger row
// that records the request.
const maxUserTitleBytes = 256

func validateUserTitle(title string) error {
	if len(title) > maxUserTitleBytes {
		return errors.Errorf("title must be at most %d bytes", maxUserTitleBytes)
	}
	return nil
}

// clampUserTitle cuts a display name that came from an identity provider down to
// the bound validateUserTitle enforces. That value is not a request this server
// may refuse — refusing the login over a long name would lock the account out —
// so it is shortened on a rune boundary instead.
func clampUserTitle(title string) string {
	if len(title) <= maxUserTitleBytes {
		return title
	}
	return common.TruncateUTF8Bytes(title, maxUserTitleBytes)
}

func extractDomain(input string) string {
	pattern := `[a-zA-Z0-9-]+(\.[a-zA-Z0-9-]+)+`
	regExp, err := regexp.Compile(pattern)
	if err != nil {
		// WHen the pattern is invalid, we just return the input.
		return input
	}

	match := regExp.FindString(input)
	domainParts := strings.Split(match, ".")
	// If the domain has at least 3 parts, we will remove the first part.
	if len(domainParts) >= 3 {
		match = strings.Join(domainParts[1:], ".")
	}
	return match
}

func isUserWorkspaceAdmin(ctx context.Context, stores *store.Store, user *store.UserMessage) (bool, error) {
	workspacePolicy, err := stores.GetWorkspaceIamPolicy(ctx)
	if err != nil {
		return false, err
	}
	roles := utils.GetUserFormattedRolesMap(ctx, stores, user, workspacePolicy.Policy)
	return roles[common.FormatRole(common.WorkspaceAdmin)], nil
}
