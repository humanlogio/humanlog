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
		NewUserAuthInterceptor(ll, tokenSource),
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

func NewUserAuthInterceptor(ll *slog.Logger, tokenSource *UserRefreshableTokenSource) connect.Interceptor {
	return &userAuthInjector{ll: ll, tokenSource: tokenSource}
}

type userAuthInjector struct {
	ll          *slog.Logger
	tokenSource *UserRefreshableTokenSource
}

func (uai *userAuthInjector) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		userToken, err := uai.tokenSource.GetUserToken(ctx)
		if err != nil {
			return nil, err
		}
		uai.ll.DebugContext(ctx, "unary auth injection", slog.String("peer.addr", req.Peer().Addr))
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
		uai.ll.DebugContext(ctx, "streaming client auth injection", slog.String("peer.addr", conn.Peer().Addr))
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
		uai.ll.DebugContext(ctx, "streaming duplex injection", slog.String("peer.addr", shc.Peer().Addr))
		shc.RequestHeader().Set("Authorization", "Bearer "+userToken.Token)
		return next(ctx, shc)
	}
}

func NewEnvironmentAuthInterceptor(ll *slog.Logger, token *typesv1.EnvironmentToken) connect.Interceptor {
	return &environmentAuthInjector{ll: ll, token: token}
}

type environmentAuthInjector struct {
	ll    *slog.Logger
	token *typesv1.EnvironmentToken
}

func (aai *environmentAuthInjector) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		aai.ll.DebugContext(ctx, "unary auth injection", slog.String("peer.addr", req.Peer().Addr))
		req.Header().Set("Authorization", "Bearer "+aai.token.Token)
		return next(ctx, req)
	}
}

func (aai *environmentAuthInjector) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return func(ctx context.Context, spec connect.Spec) connect.StreamingClientConn {
		conn := next(ctx, spec)
		aai.ll.DebugContext(ctx, "streaming client auth injection", slog.String("peer.addr", conn.Peer().Addr))
		conn.RequestHeader().Set("Authorization", "Bearer "+aai.token.Token)
		return conn
	}
}

func (aai *environmentAuthInjector) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, shc connect.StreamingHandlerConn) error {
		shc.RequestHeader().Set("Authorization", "Bearer "+aai.token.Token)
		aai.ll.DebugContext(ctx, "streaming duplex injection", slog.String("peer.addr", shc.Peer().Addr))
		return next(ctx, shc)
	}
}
