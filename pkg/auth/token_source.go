package auth

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/99designs/keyring"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"google.golang.org/protobuf/proto"
)

const (
	UserTokenKeyringKey = "user-token"
)

type UserRefreshableTokenSource struct {
	utmu sync.Mutex
	ut   *typesv1.UserToken

	getKeyring func() (keyring.Keyring, error)
}

func NewRefreshableTokenSource(getKeyring func() (keyring.Keyring, error)) *UserRefreshableTokenSource {
	return &UserRefreshableTokenSource{getKeyring: getKeyring}
}

func (rts *UserRefreshableTokenSource) ClearToken(ctx context.Context) error {
	ring, err := rts.getKeyring()
	if err != nil {
		return fmt.Errorf("opening keyring: %v", err)
	}

	rts.utmu.Lock()
	defer rts.utmu.Unlock()
	rts.ut = nil
	if err := ring.Remove(UserTokenKeyringKey); err != nil {
		return fmt.Errorf("removing credentials from keyring")
	}
	return nil
}

func (rts *UserRefreshableTokenSource) GetUserToken(ctx context.Context) (*typesv1.UserToken, error) {
	rts.utmu.Lock()
	defer rts.utmu.Unlock()
	if rts.ut != nil {
		return rts.ut, nil
	}
	ring, err := rts.getKeyring()
	if err != nil {
		return nil, fmt.Errorf("opening keyring: %v", err)
	}
	data, err := ring.Get(UserTokenKeyringKey)
	if errors.Is(err, keyring.ErrKeyNotFound) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("retrieving user token from keyring: %v", err)
	}

	var out typesv1.UserToken
	err = proto.Unmarshal(data.Data, &out)
	if err != nil {
		return nil, fmt.Errorf("marshaling token to proto: %v", err)
	}
	rts.ut = &out
	return &out, nil
}

func (rts *UserRefreshableTokenSource) RefreshUserToken(ctx context.Context, newToken string) error {
	old, err := rts.GetUserToken(ctx)
	if err != nil {
		return err
	}
	return rts.SetUserToken(ctx, &typesv1.UserToken{UserId: old.UserId, Token: newToken})
}

func (rts *UserRefreshableTokenSource) SetUserToken(ctx context.Context, userToken *typesv1.UserToken) error {
	userTokenRaw, err := proto.Marshal(userToken)
	if err != nil {
		return fmt.Errorf("marshaling token to proto: %v", err)
	}
	ring, err := rts.getKeyring()
	if err != nil {
		return fmt.Errorf("opening keyring: %v", err)
	}
	rts.utmu.Lock()
	defer rts.utmu.Unlock()
	err = ring.Set(keyring.Item{
		Key:         UserTokenKeyringKey,
		Data:        userTokenRaw,
		Label:       "humanlog.io user authentication",
		Description: "humanlog wants to store session credentials in a secure location",
	})
	if err != nil {
		return fmt.Errorf("setting user token in keyring: %v", err)
	}
	rts.ut = userToken
	return nil
}
