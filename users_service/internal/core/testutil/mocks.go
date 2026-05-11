// Package testutil contiene mocks compartidos para los tests del core.
// Cada mock implementa un puerto outbound; almacena lo que recibe en
// memoria y permite predefinir errores. No usar en producción.
package testutil

import (
	"context"
	"errors"
	"sync"
	"time"

	"users_service/internal/core/domain"
	"users_service/internal/core/ports"
	"users_service/internal/shared/search"
)

// =============== UserRepository mock ===============

type UserRepoMock struct {
	mu      sync.Mutex
	byID    map[domain.UserID]*domain.User
	byEmail map[domain.Email]*domain.User
	docs    map[string]struct{}

	SaveErr           error
	UpdateErr         error
	FindByIDErr       error
	FindByEmailErr    error
	ExistsByDocumErr  error
	SetActiveErr      error
	TouchErr          error
	SetHubspotIDErr   error
	SearchErr         error
	NextID            domain.UserID
	TouchedLastAccess []domain.UserID
}

func NewUserRepoMock() *UserRepoMock {
	return &UserRepoMock{
		byID:    map[domain.UserID]*domain.User{},
		byEmail: map[domain.Email]*domain.User{},
		docs:    map[string]struct{}{},
	}
}

// Seed inserta un user al mock como si fuera un dato pre-existente
// (saltea las reglas de Save).
func (m *UserRepoMock) Seed(u *domain.User) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.byID[u.ID] = u
	m.byEmail[u.Email] = u
	if u.DocumentNumber != "" {
		m.docs[u.DocumentNumber] = struct{}{}
	}
}

func (m *UserRepoMock) Save(_ context.Context, u *domain.User) (domain.UserID, error) {
	if m.SaveErr != nil {
		return "", m.SaveErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	id := m.NextID
	if id == "" {
		id = domain.UserID("user-" + string(u.Email))
	}
	u.ID = id
	if u.CreatedAt.IsZero() {
		u.CreatedAt = time.Now()
	}
	m.byID[id] = u
	m.byEmail[u.Email] = u
	if u.DocumentNumber != "" {
		m.docs[u.DocumentNumber] = struct{}{}
	}
	return id, nil
}

func (m *UserRepoMock) Update(_ context.Context, u *domain.User) error {
	if m.UpdateErr != nil {
		return m.UpdateErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.byID[u.ID]; !ok {
		return domain.ErrUserNotFound
	}
	m.byID[u.ID] = u
	m.byEmail[u.Email] = u
	return nil
}

func (m *UserRepoMock) FindByID(_ context.Context, id domain.UserID) (*domain.User, error) {
	if m.FindByIDErr != nil {
		return nil, m.FindByIDErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.byID[id]
	if !ok {
		return nil, domain.ErrUserNotFound
	}
	return u, nil
}

func (m *UserRepoMock) FindByEmail(_ context.Context, email domain.Email) (*domain.User, error) {
	if m.FindByEmailErr != nil {
		return nil, m.FindByEmailErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.byEmail[email.Normalize()]
	if !ok {
		return nil, domain.ErrUserNotFound
	}
	return u, nil
}

func (m *UserRepoMock) ExistsByDocument(_ context.Context, doc string) (bool, error) {
	if m.ExistsByDocumErr != nil {
		return false, m.ExistsByDocumErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.docs[doc]
	return ok, nil
}

func (m *UserRepoMock) SetActive(_ context.Context, id domain.UserID, active bool) error {
	if m.SetActiveErr != nil {
		return m.SetActiveErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.byID[id]
	if !ok {
		return domain.ErrUserNotFound
	}
	u.Active = active
	return nil
}

func (m *UserRepoMock) TouchLastAccess(_ context.Context, id domain.UserID) error {
	if m.TouchErr != nil {
		return m.TouchErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.TouchedLastAccess = append(m.TouchedLastAccess, id)
	return nil
}

func (m *UserRepoMock) SetHubspotRecordID(_ context.Context, id domain.UserID, recordID string) error {
	if m.SetHubspotIDErr != nil {
		return m.SetHubspotIDErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.byID[id]
	if !ok {
		return domain.ErrUserNotFound
	}
	u.HubspotRecordID = recordID
	return nil
}

func (m *UserRepoMock) Search(_ context.Context, _ search.Request) (*search.Response, error) {
	if m.SearchErr != nil {
		return nil, m.SearchErr
	}
	return &search.Response{}, nil
}

// ----- nuevos métodos: superadmin + password change -----

func (m *UserRepoMock) SetPassword(_ context.Context, id domain.UserID, newHash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.byID[id]
	if !ok {
		return domain.ErrUserNotFound
	}
	u.PasswordHash = newHash
	u.MustChangePassword = false
	return nil
}

func (m *UserRepoMock) ResetPassword(_ context.Context, id domain.UserID, newHash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.byID[id]
	if !ok {
		return domain.ErrUserNotFound
	}
	u.PasswordHash = newHash
	u.MustChangePassword = true
	return nil
}

func (m *UserRepoMock) SaveSuperadmin(_ context.Context, email, passwordHash string) (domain.UserID, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	em := domain.Email(email).Normalize()
	if _, exists := m.byEmail[em]; exists {
		return "", domain.ErrEmailTaken
	}
	id := domain.UserID("super-" + email)
	u := &domain.User{
		ID:                 id,
		Email:              em,
		PasswordHash:       passwordHash,
		Active:             true,
		IsSuperadmin:       true,
		MustChangePassword: true,
		CreatedAt:          time.Now(),
	}
	m.byID[id] = u
	m.byEmail[em] = u
	return id, nil
}

func (m *UserRepoMock) ExistsAnySuperadmin(_ context.Context) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, u := range m.byID {
		if u.IsSuperadmin {
			return true, nil
		}
	}
	return false, nil
}

// =============== PermissionRepository mock ===============

type PermissionRepoMock struct {
	CodesByUser map[domain.UserID][]string
	Groups      map[uint32]bool
	Assigned    map[domain.UserID]map[uint32]bool

	FindCodesErr  error
	GroupExistsErr error
	AssignErr     error
	RevokeErr     error
}

func NewPermissionRepoMock() *PermissionRepoMock {
	return &PermissionRepoMock{
		CodesByUser: map[domain.UserID][]string{},
		Groups:      map[uint32]bool{},
		Assigned:    map[domain.UserID]map[uint32]bool{},
	}
}

func (m *PermissionRepoMock) FindCodesByUserID(_ context.Context, userID domain.UserID) ([]string, error) {
	if m.FindCodesErr != nil {
		return nil, m.FindCodesErr
	}
	return m.CodesByUser[userID], nil
}

func (m *PermissionRepoMock) GroupExists(_ context.Context, groupID uint32) (bool, error) {
	if m.GroupExistsErr != nil {
		return false, m.GroupExistsErr
	}
	return m.Groups[groupID], nil
}

func (m *PermissionRepoMock) AssignGroupToUser(_ context.Context, userID domain.UserID, groupID uint32) error {
	if m.AssignErr != nil {
		return m.AssignErr
	}
	if m.Assigned[userID] == nil {
		m.Assigned[userID] = map[uint32]bool{}
	}
	m.Assigned[userID][groupID] = true
	return nil
}

func (m *PermissionRepoMock) RevokeGroupFromUser(_ context.Context, userID domain.UserID, groupID uint32) error {
	if m.RevokeErr != nil {
		return m.RevokeErr
	}
	if m.Assigned[userID] != nil {
		delete(m.Assigned[userID], groupID)
	}
	return nil
}

// ----- Métodos del CRUD de permission_group (stubs para satisfacer la
// interfaz; los handlers que los usan tienen sus propios tests dedicados) -----

func (m *PermissionRepoMock) CreateGroup(_ context.Context, _ , _ , _ string, _ []uint32) (uint32, error) {
	return 0, nil
}

func (m *PermissionRepoMock) UpdateGroup(_ context.Context, _ uint32, _, _ string) error {
	return nil
}

func (m *PermissionRepoMock) DeleteGroup(_ context.Context, _ uint32) error {
	return nil
}

func (m *PermissionRepoMock) FindGroupByID(_ context.Context, _ uint32) (*domain.PermissionGroup, error) {
	return nil, nil
}

func (m *PermissionRepoMock) ListGroups(_ context.Context) ([]domain.PermissionGroup, error) {
	return nil, nil
}

func (m *PermissionRepoMock) AddPermissionToGroup(_ context.Context, _, _ uint32) error {
	return nil
}

func (m *PermissionRepoMock) RemovePermissionFromGroup(_ context.Context, _, _ uint32) error {
	return nil
}

func (m *PermissionRepoMock) ListPermissions(_ context.Context) ([]domain.Permission, error) {
	return nil, nil
}

func (m *PermissionRepoMock) PermissionExists(_ context.Context, _ uint32) (bool, error) {
	return false, nil
}

func (m *PermissionRepoMock) GroupHasUsers(_ context.Context, _ uint32) (bool, error) {
	return false, nil
}

func (m *PermissionRepoMock) ListUsersInGroup(_ context.Context, _ ports.ListUsersInGroupInput) ([]domain.User, uint32, error) {
	return nil, 0, nil
}

// =============== UserCache mock ===============

type UserCacheMock struct {
	mu       sync.Mutex
	store    map[domain.UserID]*domain.User
	GetErr   error
	SetErr   error
	DelErr   error
	Deleted  []domain.UserID
}

func NewUserCacheMock() *UserCacheMock {
	return &UserCacheMock{store: map[domain.UserID]*domain.User{}}
}

func (m *UserCacheMock) Set(_ context.Context, u *domain.User) error {
	if m.SetErr != nil {
		return m.SetErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.store[u.ID] = u
	return nil
}

func (m *UserCacheMock) Get(_ context.Context, id domain.UserID) (*domain.User, error) {
	if m.GetErr != nil {
		return nil, m.GetErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.store[id]
	if !ok {
		return nil, errors.New("cache miss")
	}
	return u, nil
}

func (m *UserCacheMock) Delete(_ context.Context, id domain.UserID) error {
	if m.DelErr != nil {
		return m.DelErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.store, id)
	m.Deleted = append(m.Deleted, id)
	return nil
}

// =============== PasswordHasher mock ===============

type HasherMock struct {
	HashFn    func(string) (string, error)
	CompareFn func(hashed, plain string) error
}

func (m *HasherMock) Hash(plain string) (string, error) {
	if m.HashFn != nil {
		return m.HashFn(plain)
	}
	return "hashed:" + plain, nil
}

func (m *HasherMock) Compare(hashed, plain string) error {
	if m.CompareFn != nil {
		return m.CompareFn(hashed, plain)
	}
	if hashed != "hashed:"+plain {
		return errors.New("password mismatch")
	}
	return nil
}

// =============== TokenIssuer / TokenVerifier mocks ===============

type TokenIssuerMock struct {
	IssuedFor *ports.TokenIssueParams
	Pair      ports.TokenPair
	Err       error
}

func (m *TokenIssuerMock) IssuePair(p ports.TokenIssueParams) (*ports.TokenPair, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	pp := p
	m.IssuedFor = &pp
	pair := m.Pair
	if pair.AccessToken == "" {
		pair = ports.TokenPair{
			AccessToken:  "access-" + string(p.UserID),
			RefreshToken: "refresh-" + string(p.UserID),
			RefreshJTI:   "jti-" + string(p.UserID),
			AccessExp:    time.Now().Add(15 * time.Minute),
			RefreshExp:   time.Now().Add(7 * 24 * time.Hour),
		}
	}
	return &pair, nil
}

type TokenVerifierMock struct {
	Claims *ports.TokenClaims
	Err    error
}

func (m *TokenVerifierMock) Verify(_ string) (*ports.TokenClaims, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	return m.Claims, nil
}

// =============== RefreshTokenRepository mock ===============

type RefreshRepoMock struct {
	mu       sync.Mutex
	store    map[string]*ports.RefreshTokenRecord
	SaveErr  error
	FindErr  error
	RevokeErr error
}

func NewRefreshRepoMock() *RefreshRepoMock {
	return &RefreshRepoMock{store: map[string]*ports.RefreshTokenRecord{}}
}

func (m *RefreshRepoMock) Save(_ context.Context, r *ports.RefreshTokenRecord) error {
	if m.SaveErr != nil {
		return m.SaveErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	rec := *r
	m.store[r.JTI] = &rec
	return nil
}

func (m *RefreshRepoMock) FindByJTI(_ context.Context, jti string) (*ports.RefreshTokenRecord, error) {
	if m.FindErr != nil {
		return nil, m.FindErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.store[jti]
	if !ok {
		return nil, domain.ErrInvalidRefreshToken
	}
	rec := *r
	return &rec, nil
}

func (m *RefreshRepoMock) Revoke(_ context.Context, jti, replacedBy string) error {
	if m.RevokeErr != nil {
		return m.RevokeErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.store[jti]
	if !ok {
		return nil
	}
	now := time.Now()
	r.RevokedAt = &now
	r.ReplacedBy = replacedBy
	return nil
}

func (m *RefreshRepoMock) RevokeAllForUser(_ context.Context, userID domain.UserID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	for _, r := range m.store {
		if r.UserID == userID && r.RevokedAt == nil {
			r.RevokedAt = &now
		}
	}
	return nil
}
