package auth

import (
	"context"
	"log/slog"

	"connectrpc.com/connect"
	typesv1 "github.com/humanlogio/api/go/types/v1"
)

func Interceptors(ll *slog.Logger, tokenSource *UserRefreshableTokenSource) []connect.Interceptor {
	return []connect.Interceptor{
		NewRefreshedUserAuthInterceptor(ll, tokenSource),
		NewUserAuthInterceptor(tokenSource),
	}
}

func NewRefreshedUserAuthInterceptor(ll *slog.Logger, tokenSource *UserRefreshableTokenSource) connect.Interceptor {
	return &refreshedTokenInterceptor{ll: ll, tokenSource: tokenSource}
}

type refreshedTokenInterceptor struct {
	ll          *slog.Logger
	tokenSource *UserRefreshableTokenSource
}

func (rti *refreshedTokenInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		res, err := next(ctx, req)
		if res != nil {
			if refreshedToken := res.Header().Get("UseAuthorization"); refreshedToken != "" {
				rti.ll.DebugContext(ctx, "received refreshed token")
				if err := rti.tokenSource.RefreshUserToken(ctx, refreshedToken); err != nil {
					rti.ll.ErrorContext(ctx, "refreshing user token", slog.Any("err", err))
				}
			}
		}
		return res, err
	}
}

func (rti *refreshedTokenInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return func(ctx context.Context, spec connect.Spec) connect.StreamingClientConn {
		conn := next(ctx, spec)

		if refreshedToken := conn.ResponseHeader().Get("UseAuthorization"); refreshedToken != "" {
			rti.ll.DebugContext(ctx, "received refreshed token")
			if err := rti.tokenSource.RefreshUserToken(ctx, refreshedToken); err != nil {
				rti.ll.ErrorContext(ctx, "refreshing user token", slog.Any("err", err))
			}
		}
		return conn
	}
}

func (rti *refreshedTokenInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, shc connect.StreamingHandlerConn) error {
		if refreshedToken := shc.ResponseHeader().Get("UseAuthorization"); refreshedToken != "" {
			rti.ll.DebugContext(ctx, "received refreshed token")
			if err := rti.tokenSource.RefreshUserToken(ctx, refreshedToken); err != nil {
				rti.ll.ErrorContext(ctx, "refreshing user token", slog.Any("err", err))
			}
		}
		err := next(ctx, shc)
		if refreshedToken := shc.ResponseTrailer().Get("UseAuthorization"); refreshedToken != "" {
			rti.ll.DebugContext(ctx, "received refreshed token")
			if err := rti.tokenSource.RefreshUserToken(ctx, refreshedToken); err != nil {
				rti.ll.ErrorContext(ctx, "refreshing user token", slog.Any("err", err))
			}
		}
		return err
	}
}

func NewUserAuthInterceptor(tokenSource *UserRefreshableTokenSource) connect.Interceptor {
	return &userAuthInjector{tokenSource: tokenSource}
}

type userAuthInjector struct {
	tokenSource *UserRefreshableTokenSource
}

func (uai *userAuthInjector) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		userToken, err := uai.tokenSource.GetUserToken(ctx)
		if err != nil {
			return nil, err
		}

		req.Header().Set("Authorization", "Bearer "+userToken.Token)
		return next(ctx, req)
	}
}

func (uai *userAuthInjector) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return func(ctx context.Context, spec connect.Spec) connect.StreamingClientConn {
		conn := next(ctx, spec)
		userToken, err := uai.tokenSource.GetUserToken(ctx)
		if err != nil {
			panic(err)
		}
		conn.RequestHeader().Set("Authorization", "Bearer "+userToken.Token)
		return conn
	}
}

func (uai *userAuthInjector) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, shc connect.StreamingHandlerConn) error {
		userToken, err := uai.tokenSource.GetUserToken(ctx)
		if err != nil {
			return err
		}
		shc.RequestHeader().Set("Authorization", "Bearer "+userToken.Token)
		return next(ctx, shc)
	}
}

func NewAccountAuthInterceptor(token *typesv1.AccountToken) connect.Interceptor {
	return &accountAuthInjector{token: token}
}

type accountAuthInjector struct {
	token *typesv1.AccountToken
}

func (aai *accountAuthInjector) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		req.Header().Set("Authorization", "Bearer "+aai.token.Token)
		return next(ctx, req)
	}
}

func (aai *accountAuthInjector) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return func(ctx context.Context, spec connect.Spec) connect.StreamingClientConn {
		conn := next(ctx, spec)
		conn.RequestHeader().Set("Authorization", "Bearer "+aai.token.Token)
		return conn
	}
}

func (aai *accountAuthInjector) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, shc connect.StreamingHandlerConn) error {
		shc.RequestHeader().Set("Authorization", "Bearer "+aai.token.Token)
		return next(ctx, shc)
	}
}
