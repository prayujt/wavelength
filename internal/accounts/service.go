// Package accounts manages local accounts independently of their authentication protocol.
package accounts

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/prayujt/wavelength/internal/state"
)

var (
	ErrInvalid   = errors.New("invalid account settings")
	ErrForbidden = errors.New("account operation is not permitted")
	ErrExists    = errors.New("account already exists")
)

type Account struct {
	Username, Email string
	Roles           map[string]bool
	Folders         []string
	MaxBitRate      int
}

type storedAccount struct {
	Account
	Password string
}

type Changes struct {
	Email, Password *string
	Roles           map[string]bool
	Folders         *[]string
	MaxBitRate      *int
}

type Service struct {
	store              state.Store
	key                []byte
	username, password string
}

func New(store state.Store, key []byte, username, password string) (*Service, error) {
	if len(key) != 32 || username == "" || password == "" {
		return nil, ErrInvalid
	}
	return &Service{store: store, key: slices.Clone(key), username: username, password: password}, nil
}

func (s *Service) bootstrap() Account {
	return Account{
		Username: s.username,
		Roles:    map[string]bool{"adminRole": true, "settingsRole": true, "streamRole": true, "downloadRole": true, "playlistRole": true, "commentRole": true},
		Folders:  []string{"1"},
	}
}

func (s *Service) record(ctx context.Context, name string) (storedAccount, error) {
	user, err := state.Read[storedAccount](ctx, s.store, "subsonic-users", name)
	if errors.Is(err, state.ErrNotFound) && name == s.username {
		return storedAccount{Account: s.bootstrap()}, nil
	}
	return user, err
}

func (s *Service) Get(ctx context.Context, name string) (Account, error) {
	user, err := s.record(ctx, name)
	return user.Account, err
}

// Verify lets the boundary supply its credential scheme without exposing stored secrets.
func (s *Service) Verify(ctx context.Context, name string, verify func(string) bool) (Account, bool, error) {
	user, err := s.record(ctx, name)
	if errors.Is(err, state.ErrNotFound) {
		return Account{}, false, nil
	}
	if err != nil {
		return Account{}, false, err
	}
	password := s.password
	if user.Password != "" {
		password, err = s.unseal(user.Password)
		if err != nil {
			return Account{}, false, err
		}
	} else if name != s.username {
		return Account{}, false, nil
	}
	return user.Account, verify(password), nil
}

func (s *Service) List(ctx context.Context) ([]Account, error) {
	records, err := state.ReadAll[storedAccount](ctx, s.store, "subsonic-users")
	if err != nil {
		return nil, err
	}
	if _, ok := records[s.username]; !ok {
		records[s.username] = storedAccount{Account: s.bootstrap()}
	}
	users := []Account{}
	for _, record := range records {
		users = append(users, record.Account)
	}
	slices.SortFunc(users, func(a, b Account) int { return strings.Compare(a.Username, b.Username) })
	return users, nil
}

func (s *Service) Create(ctx context.Context, name string, changes Changes) error {
	if name == "" || changes.Password == nil || changes.Email == nil {
		return ErrInvalid
	}
	if name == s.username {
		return ErrExists
	}
	return s.change(ctx, name, changes, true)
}

func (s *Service) Update(ctx context.Context, name string, changes Changes) error {
	return s.change(ctx, name, changes, false)
}

func (s *Service) change(ctx context.Context, name string, changes Changes, create bool) error {
	if changes.MaxBitRate != nil && !slices.Contains([]int{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320}, *changes.MaxBitRate) {
		return ErrInvalid
	}
	if changes.Roles["jukeboxRole"] {
		return ErrForbidden
	}
	password := ""
	if changes.Password != nil {
		if *changes.Password == "" {
			return ErrInvalid
		}
		var err error
		password, err = s.seal(*changes.Password)
		if err != nil {
			return err
		}
	}
	return state.Modify(ctx, s.store, "subsonic-users", name, func(user *storedAccount, exists bool) error {
		if create && exists {
			return ErrExists
		}
		if !create && !exists && name != s.username {
			return state.ErrNotFound
		}
		if !exists {
			user.Account = Account{Username: name, Roles: map[string]bool{"settingsRole": true, "streamRole": true}, Folders: []string{"1"}}
			if name == s.username {
				user.Account = s.bootstrap()
			}
		}
		if user.Roles == nil {
			user.Roles = map[string]bool{}
		}
		if changes.Email != nil {
			user.Email = *changes.Email
		}
		if changes.Password != nil {
			user.Password = password
		}
		if changes.MaxBitRate != nil {
			user.MaxBitRate = *changes.MaxBitRate
		}
		if changes.Folders != nil {
			user.Folders = slices.Clone(*changes.Folders)
		}
		for role, enabled := range changes.Roles {
			user.Roles[role] = enabled
		}
		user.Roles["jukeboxRole"] = false
		if name == s.username {
			user.Roles["adminRole"] = true
		}
		return nil
	})
}

func (s *Service) Delete(ctx context.Context, name string) error {
	if name == s.username {
		return ErrForbidden
	}
	if _, err := s.Get(ctx, name); err != nil {
		return err
	}
	return state.Delete(ctx, s.store, "subsonic-users", name)
}
