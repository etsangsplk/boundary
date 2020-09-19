package session

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/golang/protobuf/ptypes"
	"github.com/hashicorp/boundary/internal/db"
	"github.com/hashicorp/boundary/internal/iam"
	"github.com/hashicorp/boundary/internal/kms"
	"github.com/hashicorp/boundary/internal/oplog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRepository_ListSession(t *testing.T) {
	t.Parallel()
	conn, _ := db.TestSetup(t, "postgres")
	const testLimit = 10
	wrapper := db.TestWrapper(t)
	iamRepo := iam.TestRepo(t, conn, wrapper)
	rw := db.New(conn)
	kms := kms.TestKms(t, conn, wrapper)
	repo, err := NewRepository(rw, rw, kms, WithLimit(testLimit))
	require.NoError(t, err)
	composedOf := TestSessionParams(t, conn, wrapper, iamRepo)

	type args struct {
		opt []Option
	}
	tests := []struct {
		name      string
		createCnt int
		args      args
		wantCnt   int
		wantErr   bool
	}{
		{
			name:      "no-limit",
			createCnt: repo.defaultLimit + 1,
			args: args{
				opt: []Option{WithLimit(-1)},
			},
			wantCnt: repo.defaultLimit + 1,
			wantErr: false,
		},
		{
			name:      "default-limit",
			createCnt: repo.defaultLimit + 1,
			args:      args{},
			wantCnt:   repo.defaultLimit,
			wantErr:   false,
		},
		{
			name:      "custom-limit",
			createCnt: repo.defaultLimit + 1,
			args: args{
				opt: []Option{WithLimit(3)},
			},
			wantCnt: 3,
			wantErr: false,
		},
		{
			name:      "withScopeId",
			createCnt: repo.defaultLimit + 1,
			args: args{
				opt: []Option{WithScopeId(composedOf.ScopeId)},
			},
			wantCnt: repo.defaultLimit,
			wantErr: false,
		},
		{
			name:      "bad-withScopeId",
			createCnt: repo.defaultLimit + 1,
			args: args{
				opt: []Option{WithScopeId("o_thisIsNotValid")},
			},
			wantCnt: 0,
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert, require := assert.New(t), require.New(t)
			require.NoError(conn.Where("1=1").Delete(AllocSession()).Error)
			testSessions := []*Session{}
			for i := 0; i < tt.createCnt; i++ {
				s := TestSession(t, conn, wrapper, composedOf)
				testSessions = append(testSessions, s)
			}
			assert.Equal(tt.createCnt, len(testSessions))
			got, err := repo.ListSessions(context.Background(), tt.args.opt...)
			if tt.wantErr {
				require.Error(err)
				return
			}
			require.NoError(err)
			assert.Equal(tt.wantCnt, len(got))
		})
	}
	t.Run("withOrder", func(t *testing.T) {
		assert, require := assert.New(t), require.New(t)
		require.NoError(conn.Where("1=1").Delete(AllocSession()).Error)
		wantCnt := 5
		for i := 0; i < wantCnt; i++ {
			_ = TestSession(t, conn, wrapper, composedOf)
		}
		got, err := repo.ListSessions(context.Background(), WithOrder("create_time asc"))
		require.NoError(err)
		assert.Equal(wantCnt, len(got))

		for i := 0; i < len(got)-1; i++ {
			first, err := ptypes.Timestamp(got[i].CreateTime.Timestamp)
			require.NoError(err)
			second, err := ptypes.Timestamp(got[i+1].CreateTime.Timestamp)
			require.NoError(err)
			assert.True(first.Before(second))
		}
	})
	t.Run("withUserId", func(t *testing.T) {
		assert, require := assert.New(t), require.New(t)
		require.NoError(conn.Where("1=1").Delete(AllocSession()).Error)
		wantCnt := 5
		for i := 0; i < wantCnt; i++ {
			_ = TestSession(t, conn, wrapper, composedOf)
		}
		s := TestDefaultSession(t, conn, wrapper, iamRepo)
		got, err := repo.ListSessions(context.Background(), WithUserId(s.UserId))
		require.NoError(err)
		assert.Equal(1, len(got))
		assert.Equal(got[0].UserId, s.UserId)
	})
}

func TestRepository_CreateSession(t *testing.T) {
	t.Parallel()
	conn, _ := db.TestSetup(t, "postgres")
	rw := db.New(conn)
	wrapper := db.TestWrapper(t)
	iamRepo := iam.TestRepo(t, conn, wrapper)
	kms := kms.TestKms(t, conn, wrapper)
	repo, err := NewRepository(rw, rw, kms)
	require.NoError(t, err)

	type args struct {
		composedOf ComposedOf
	}
	tests := []struct {
		name        string
		args        args
		wantErr     bool
		wantIsError error
	}{
		{
			name: "valid",
			args: args{
				composedOf: TestSessionParams(t, conn, wrapper, iamRepo),
			},
			wantErr: false,
		},
		{
			name: "empty-userId",
			args: args{
				composedOf: func() ComposedOf {
					c := TestSessionParams(t, conn, wrapper, iamRepo)
					c.UserId = ""
					return c
				}(),
			},
			wantErr:     true,
			wantIsError: db.ErrInvalidParameter,
		},
		{
			name: "empty-hostId",
			args: args{
				composedOf: func() ComposedOf {
					c := TestSessionParams(t, conn, wrapper, iamRepo)
					c.HostId = ""
					return c
				}(),
			},
			wantErr:     true,
			wantIsError: db.ErrInvalidParameter,
		},
		{
			name: "empty-targetId",
			args: args{
				composedOf: func() ComposedOf {
					c := TestSessionParams(t, conn, wrapper, iamRepo)
					c.TargetId = ""
					return c
				}(),
			},
			wantErr:     true,
			wantIsError: db.ErrInvalidParameter,
		},
		{
			name: "empty-hostSetId",
			args: args{
				composedOf: func() ComposedOf {
					c := TestSessionParams(t, conn, wrapper, iamRepo)
					c.HostSetId = ""
					return c
				}(),
			},
			wantErr:     true,
			wantIsError: db.ErrInvalidParameter,
		},
		{
			name: "empty-authTokenId",
			args: args{
				composedOf: func() ComposedOf {
					c := TestSessionParams(t, conn, wrapper, iamRepo)
					c.AuthTokenId = ""
					return c
				}(),
			},
			wantErr:     true,
			wantIsError: db.ErrInvalidParameter,
		},
		{
			name: "empty-scopeId",
			args: args{
				composedOf: func() ComposedOf {
					c := TestSessionParams(t, conn, wrapper, iamRepo)
					c.ScopeId = ""
					return c
				}(),
			},
			wantErr:     true,
			wantIsError: db.ErrInvalidParameter,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert, require := assert.New(t), require.New(t)
			s := &Session{
				UserId:      tt.args.composedOf.UserId,
				HostId:      tt.args.composedOf.HostId,
				TargetId:    tt.args.composedOf.TargetId,
				HostSetId:   tt.args.composedOf.HostSetId,
				AuthTokenId: tt.args.composedOf.AuthTokenId,
				ScopeId:     tt.args.composedOf.ScopeId,
				Endpoint:    "tcp://127.0.0.1:22",
			}
			ses, st, privKey, err := repo.CreateSession(context.Background(), wrapper, s)
			if tt.wantErr {
				assert.Error(err)
				assert.Nil(ses)
				assert.Nil(st)
				if tt.wantIsError != nil {
					assert.True(errors.Is(err, tt.wantIsError))
				}
				return
			}
			require.NoError(err)
			assert.NotNil(ses)
			assert.NotNil(privKey)
			assert.NotNil(st)
			assert.NotNil(ses.CreateTime)
			assert.NotNil(st.StartTime)
			assert.Equal(st.Status, StatusPending.String())
			foundSession, foundStates, err := repo.LookupSession(context.Background(), ses.PublicId)
			assert.NoError(err)
			assert.Equal(foundSession, ses)

			err = db.TestVerifyOplog(t, rw, ses.PublicId, db.WithOperation(oplog.OpType_OP_TYPE_CREATE), db.WithCreateNotBefore(10*time.Second))
			assert.Error(err)

			require.Equal(1, len(foundStates))
			assert.Equal(foundStates[0].Status, StatusPending.String())
		})
	}
}

func TestRepository_UpdateState(t *testing.T) {
	t.Parallel()
	conn, _ := db.TestSetup(t, "postgres")
	rw := db.New(conn)
	wrapper := db.TestWrapper(t)
	iamRepo := iam.TestRepo(t, conn, wrapper)
	kms := kms.TestKms(t, conn, wrapper)
	repo, err := NewRepository(rw, rw, kms)
	require.NoError(t, err)

	tests := []struct {
		name                   string
		session                *Session
		newStatus              Status
		overrideSessionId      *string
		overrideSessionVersion *uint32
		wantStateCnt           int
		wantErr                bool
		wantIsError            error
	}{
		{
			name:         "cancelling",
			session:      TestDefaultSession(t, conn, wrapper, iamRepo),
			newStatus:    StatusCancelling,
			wantStateCnt: 2,
			wantErr:      false,
		},
		{
			name: "closed",
			session: func() *Session {
				s := TestDefaultSession(t, conn, wrapper, iamRepo)
				_ = TestState(t, conn, s.PublicId, StatusActive)
				return s
			}(),
			newStatus:    StatusTerminated,
			wantStateCnt: 3,
			wantErr:      false,
		},
		{
			name:      "bad-version",
			session:   TestDefaultSession(t, conn, wrapper, iamRepo),
			newStatus: StatusCancelling,
			overrideSessionVersion: func() *uint32 {
				v := uint32(22)
				return &v
			}(),
			wantErr: true,
		},
		{
			name:      "empty-version",
			session:   TestDefaultSession(t, conn, wrapper, iamRepo),
			newStatus: StatusCancelling,
			overrideSessionVersion: func() *uint32 {
				v := uint32(0)
				return &v
			}(),
			wantErr:     true,
			wantIsError: db.ErrInvalidParameter,
		},
		{
			name:      "bad-sessionId",
			session:   TestDefaultSession(t, conn, wrapper, iamRepo),
			newStatus: StatusCancelling,
			overrideSessionId: func() *string {
				s := "s_thisIsNotValid"
				return &s
			}(),
			wantErr: true,
		},
		{
			name:      "empty-session",
			session:   TestDefaultSession(t, conn, wrapper, iamRepo),
			newStatus: StatusCancelling,
			overrideSessionId: func() *string {
				s := ""
				return &s
			}(),
			wantErr:     true,
			wantIsError: db.ErrInvalidParameter,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert, require := assert.New(t), require.New(t)
			var id string
			var version uint32
			switch {
			case tt.overrideSessionId != nil:
				id = *tt.overrideSessionId
			default:
				id = tt.session.PublicId
			}
			switch {
			case tt.overrideSessionVersion != nil:
				version = *tt.overrideSessionVersion
			default:
				version = tt.session.Version
			}

			s, ss, err := repo.updateState(context.Background(), id, version, tt.newStatus)
			if tt.wantErr {
				require.Error(err)
				if tt.wantIsError != nil {
					assert.Truef(errors.Is(err, tt.wantIsError), "unexpected error %s", err.Error())
				}
				return
			}
			require.NoError(err)
			require.NotNil(s)
			require.NotNil(ss)
			assert.Equal(tt.wantStateCnt, len(ss))
			assert.Equal(tt.newStatus.String(), ss[0].Status)
		})
	}
}

func TestRepository_ConnectSession(t *testing.T) {
	t.Parallel()
	conn, _ := db.TestSetup(t, "postgres")
	rw := db.New(conn)
	wrapper := db.TestWrapper(t)
	iamRepo := iam.TestRepo(t, conn, wrapper)
	kms := kms.TestKms(t, conn, wrapper)
	repo, err := NewRepository(rw, rw, kms)
	require.NoError(t, err)

	setupFn := func() ConnectWith {
		s := TestDefaultSession(t, conn, wrapper, iamRepo)
		srv := TestWorker(t, conn, wrapper)
		tofu := TestTofu(t)
		_, _, err := repo.ActivateSession(context.Background(), s.PublicId, s.Version, srv.PrivateId, srv.Type, tofu)
		require.NoError(t, err)
		_ = TestConnection(t, conn, s.PublicId, "127.0.0.1", 22, "127.0.0.1", 2222)
		return ConnectWith{
			SessionId:          s.PublicId,
			ClientTcpAddress:   "127.0.0.1",
			ClientTcpPort:      22,
			EndpointTcpAddress: "127.0.0.1",
			EndpointTcpPort:    2222,
		}
	}
	tests := []struct {
		name        string
		connectWith ConnectWith
		wantErr     bool
		wantIsError error
	}{
		{
			name:        "valid",
			connectWith: setupFn(),
		},
		{
			name: "empty-SessionId",
			connectWith: func() ConnectWith {
				cw := setupFn()
				cw.SessionId = ""
				return cw
			}(),
			wantErr:     true,
			wantIsError: db.ErrInvalidParameter,
		},
		{
			name: "empty-ClientTcpAddress",
			connectWith: func() ConnectWith {
				cw := setupFn()
				cw.ClientTcpAddress = ""
				return cw
			}(),
			wantErr:     true,
			wantIsError: db.ErrInvalidParameter,
		},
		{
			name: "empty-ClientTcpPort",
			connectWith: func() ConnectWith {
				cw := setupFn()
				cw.ClientTcpPort = 0
				return cw
			}(),
			wantErr:     true,
			wantIsError: db.ErrInvalidParameter,
		},
		{
			name: "empty-EndpointTcpAddress",
			connectWith: func() ConnectWith {
				cw := setupFn()
				cw.EndpointTcpAddress = ""
				return cw
			}(),
			wantErr:     true,
			wantIsError: db.ErrInvalidParameter,
		},
		{
			name: "empty-EndpointTcpPort",
			connectWith: func() ConnectWith {
				cw := setupFn()
				cw.EndpointTcpPort = 0
				return cw
			}(),
			wantErr:     true,
			wantIsError: db.ErrInvalidParameter,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert, require := assert.New(t), require.New(t)

			c, cs, err := repo.ConnectSession(context.Background(), tt.connectWith)
			if tt.wantErr {
				require.Error(err)
				if tt.wantIsError != nil {
					assert.Truef(errors.Is(err, tt.wantIsError), "unexpected error %s", err.Error())
				}
				return
			}
			require.NoError(err)
			require.NotNil(c)
			require.NotNil(cs)
			assert.Equal(StatusConnected.String(), cs[0].Status)
		})
	}
}

func TestRepository_TerminateSession(t *testing.T) {
	t.Parallel()
	conn, _ := db.TestSetup(t, "postgres")
	rw := db.New(conn)
	wrapper := db.TestWrapper(t)
	iamRepo := iam.TestRepo(t, conn, wrapper)
	kms := kms.TestKms(t, conn, wrapper)
	repo, err := NewRepository(rw, rw, kms)
	require.NoError(t, err)

	setupFn := func() *Session {
		s := TestDefaultSession(t, conn, wrapper, iamRepo)
		srv := TestWorker(t, conn, wrapper)
		tofu := TestTofu(t)
		s, _, err := repo.ActivateSession(context.Background(), s.PublicId, s.Version, srv.PrivateId, srv.Type, tofu)
		require.NoError(t, err)
		return s
	}
	tests := []struct {
		name        string
		session     *Session
		reason      TerminationReason
		wantErr     bool
		wantIsError error
	}{
		{
			name:    "valid-active-session",
			session: setupFn(),
			reason:  ClosedByUser,
		},
		{
			name:    "valid-pending-session",
			session: TestDefaultSession(t, conn, wrapper, iamRepo),
			reason:  ClosedByUser,
		},
		{
			name: "empty-session-id",
			session: func() *Session {
				s := setupFn()
				s.PublicId = ""
				return s
			}(),
			reason:      ClosedByUser,
			wantErr:     true,
			wantIsError: db.ErrInvalidParameter,
		},
		{
			name: "empty-session-version",
			session: func() *Session {
				s := setupFn()
				s.Version = 0
				return s
			}(),
			reason:      ClosedByUser,
			wantErr:     true,
			wantIsError: db.ErrInvalidParameter,
		},
		{
			name: "open-connection",
			session: func() *Session {
				s := setupFn()
				_ = TestConnection(t, conn, s.PublicId, "127.0.0.1", 22, "127.0.0.1", 222)
				return s
			}(),
			reason:  ClosedByUser,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert, require := assert.New(t), require.New(t)
			s, ss, err := repo.TerminateSession(context.Background(), tt.session.PublicId, tt.session.Version, tt.reason)
			if tt.wantErr {
				require.Error(err)
				if tt.wantIsError != nil {
					assert.Truef(errors.Is(err, tt.wantIsError), "unexpected error %s", err.Error())
				}
				return
			}
			require.NoError(err)
			assert.Equal(tt.reason.String(), s.TerminationReason)
			assert.Equal(StatusTerminated.String(), ss[0].Status)
		})
	}
}

func TestRepository_CloseConnections(t *testing.T) {
	t.Parallel()
	conn, _ := db.TestSetup(t, "postgres")
	rw := db.New(conn)
	wrapper := db.TestWrapper(t)
	iamRepo := iam.TestRepo(t, conn, wrapper)
	kms := kms.TestKms(t, conn, wrapper)
	repo, err := NewRepository(rw, rw, kms)
	require.NoError(t, err)

	setupFn := func(cnt int) []CloseWith {
		s := TestDefaultSession(t, conn, wrapper, iamRepo)
		srv := TestWorker(t, conn, wrapper)
		tofu := TestTofu(t)
		s, _, err := repo.ActivateSession(context.Background(), s.PublicId, s.Version, srv.PrivateId, srv.Type, tofu)
		require.NoError(t, err)
		cw := make([]CloseWith, 0, cnt)
		for i := 0; i < cnt; i++ {
			c := TestConnection(t, conn, s.PublicId, "127.0.0.1", 22, "127.0.0.1", 2222)
			require.NoError(t, err)
			cw = append(cw, CloseWith{
				ConnectionId:      c.PublicId,
				ConnectionVersion: c.Version,
				BytesUp:           1,
				BytesDown:         2,
				ClosedReason:      ConnectionClosedByUser,
			})
		}
		return cw
	}
	tests := []struct {
		name        string
		closeWith   []CloseWith
		reason      TerminationReason
		wantErr     bool
		wantIsError error
	}{
		{
			name:      "valid",
			closeWith: setupFn(2),
			reason:    ClosedByUser,
		},
		{
			name:        "empty-closed-with",
			closeWith:   []CloseWith{},
			reason:      ClosedByUser,
			wantErr:     true,
			wantIsError: db.ErrInvalidParameter,
		},
		{
			name: "missing-ConnectionId",
			closeWith: func() []CloseWith {
				cw := setupFn(2)
				cw[1].ConnectionId = ""
				return cw

			}(),
			reason:      ClosedByUser,
			wantErr:     true,
			wantIsError: db.ErrInvalidParameter,
		},
		{
			name: "ConnectionVersion",
			closeWith: func() []CloseWith {
				cw := setupFn(2)
				cw[1].ConnectionVersion = 0
				return cw

			}(),
			reason:      ClosedByUser,
			wantErr:     true,
			wantIsError: db.ErrInvalidParameter,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert, require := assert.New(t), require.New(t)
			resp, err := repo.CloseConnections(context.Background(), tt.closeWith)
			if tt.wantErr {
				require.Error(err)
				if tt.wantIsError != nil {
					assert.Truef(errors.Is(err, tt.wantIsError), "unexpected error %s", err.Error())
				}
				return
			}
			require.NoError(err)
			assert.Equal(len(tt.closeWith), len(resp))
			for _, r := range resp {
				require.NotNil(r.Connection)
				require.NotNil(r.ConnectionStates)
				assert.Equal(StatusClosed.String(), r.ConnectionStates[0].Status)
			}
		})
	}
}

func TestRepository_CancelSession(t *testing.T) {
	t.Parallel()
	conn, _ := db.TestSetup(t, "postgres")
	rw := db.New(conn)
	wrapper := db.TestWrapper(t)
	iamRepo := iam.TestRepo(t, conn, wrapper)
	kms := kms.TestKms(t, conn, wrapper)
	repo, err := NewRepository(rw, rw, kms)
	require.NoError(t, err)
	setupFn := func() *Session {
		session := TestDefaultSession(t, conn, wrapper, iamRepo)
		_ = TestConnection(t, conn, session.PublicId, "127.0.0.1", 22, "127.0.0.1", 2222)
		return session
	}
	tests := []struct {
		name                   string
		session                *Session
		overrideSessionId      *string
		overrideSessionVersion *uint32
		wantErr                bool
		wantIsError            error
	}{
		{
			name:    "valid",
			session: setupFn(),
		},
		{
			name:    "bad-session-id",
			session: setupFn(),
			overrideSessionId: func() *string {
				id, err := newId()
				require.NoError(t, err)
				return &id
			}(),
			wantErr: true,
		},
		{
			name:    "missing-session-id",
			session: setupFn(),
			overrideSessionId: func() *string {
				id := ""
				return &id
			}(),
			wantErr:     true,
			wantIsError: db.ErrInvalidParameter,
		},
		{
			name:    "bad-version-id",
			session: setupFn(),
			overrideSessionVersion: func() *uint32 {
				v := uint32(101)
				return &v
			}(),
			wantErr: true,
		},
		{
			name:    "missing-version-id",
			session: setupFn(),
			overrideSessionVersion: func() *uint32 {
				v := uint32(0)
				return &v
			}(),
			wantErr:     true,
			wantIsError: db.ErrInvalidParameter,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert, require := assert.New(t), require.New(t)
			var id string
			var version uint32
			switch {
			case tt.overrideSessionId != nil:
				id = *tt.overrideSessionId
			default:
				id = tt.session.PublicId
			}
			switch {
			case tt.overrideSessionVersion != nil:
				version = *tt.overrideSessionVersion
			default:
				version = tt.session.Version
			}

			s, ss, err := repo.CancelSession(context.Background(), id, version)
			if tt.wantErr {
				require.Error(err)
				if tt.wantIsError != nil {
					assert.Truef(errors.Is(err, tt.wantIsError), "unexpected error %s", err.Error())
				}
				return
			}
			require.NoError(err)
			require.NotNil(s)
			require.NotNil(ss)
			assert.Equal(StatusCancelling.String(), ss[0].Status)
		})
	}
}
func TestRepository_ActivateSession(t *testing.T) {
	t.Parallel()
	conn, _ := db.TestSetup(t, "postgres")
	rw := db.New(conn)
	wrapper := db.TestWrapper(t)
	iamRepo := iam.TestRepo(t, conn, wrapper)
	kms := kms.TestKms(t, conn, wrapper)
	repo, err := NewRepository(rw, rw, kms)
	require.NoError(t, err)
	worker := TestWorker(t, conn, wrapper)

	tofu := TestTofu(t)
	tests := []struct {
		name                   string
		session                *Session
		overrideSessionId      *string
		overrideSessionVersion *uint32
		wantErr                bool
		wantIsError            error
	}{
		{
			name:    "valid",
			session: TestDefaultSession(t, conn, wrapper, iamRepo),
			wantErr: false,
		},
		{
			name: "already-active",
			session: func() *Session {
				s := TestDefaultSession(t, conn, wrapper, iamRepo)
				activeSession, _, err := repo.ActivateSession(context.Background(), s.PublicId, s.Version, worker.PrivateId, worker.Type, tofu)
				require.NoError(t, err)
				return activeSession
			}(),
			wantErr: true,
		},
		{
			name:    "bad-session-id",
			session: TestDefaultSession(t, conn, wrapper, iamRepo),
			overrideSessionId: func() *string {
				id, err := newId()
				require.NoError(t, err)
				return &id
			}(),
			wantErr: true,
		},
		{
			name:    "bad-session-version",
			session: TestDefaultSession(t, conn, wrapper, iamRepo),
			overrideSessionVersion: func() *uint32 {
				v := uint32(100)
				return &v
			}(),
			wantErr: true,
		},
		{
			name:    "empty-session-id",
			session: TestDefaultSession(t, conn, wrapper, iamRepo),
			overrideSessionId: func() *string {
				id := ""
				return &id
			}(),
			wantErr:     true,
			wantIsError: db.ErrInvalidParameter,
		},
		{
			name:    "empty-session-version",
			session: TestDefaultSession(t, conn, wrapper, iamRepo),
			overrideSessionVersion: func() *uint32 {
				v := uint32(0)
				return &v
			}(),
			wantErr:     true,
			wantIsError: db.ErrInvalidParameter,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert, require := assert.New(t), require.New(t)
			var id string
			var version uint32
			switch {
			case tt.overrideSessionId != nil:
				id = *tt.overrideSessionId
			default:
				id = tt.session.PublicId
			}
			switch {
			case tt.overrideSessionVersion != nil:
				version = *tt.overrideSessionVersion
			default:
				version = tt.session.Version
			}
			s, ss, err := repo.ActivateSession(context.Background(), id, version, worker.PrivateId, worker.Type, tofu)
			if tt.wantErr {
				require.Error(err)
				if tt.wantIsError != nil {
					assert.Truef(errors.Is(err, tt.wantIsError), "unexpected error %s", err.Error())
				}
				return
			}
			require.NoError(err)
			require.NotNil(s)
			require.NotNil(ss)
			assert.Equal(tofu, s.TofuToken)
			assert.Equal(2, len(ss))
			assert.Equal(StatusActive.String(), ss[0].Status)
		})
		t.Run("already active", func(t *testing.T) {
			assert, require := assert.New(t), require.New(t)
			session := TestDefaultSession(t, conn, wrapper, iamRepo)
			s, ss, err := repo.ActivateSession(context.Background(), session.PublicId, 1, worker.PrivateId, worker.Type, tofu)
			require.NoError(err)
			require.NotNil(s)
			require.NotNil(ss)
			assert.Equal(2, len(ss))
			assert.Equal(StatusActive.String(), ss[0].Status)

			_, _, err = repo.ActivateSession(context.Background(), session.PublicId, 1, worker.PrivateId, worker.Type, tofu)
			require.Error(err)

			_, _, err = repo.ActivateSession(context.Background(), session.PublicId, 2, worker.PrivateId, worker.Type, tofu)
			require.Error(err)
		})
	}
}

func TestRepository_DeleteSession(t *testing.T) {
	t.Parallel()
	conn, _ := db.TestSetup(t, "postgres")
	rw := db.New(conn)
	wrapper := db.TestWrapper(t)
	iamRepo := iam.TestRepo(t, conn, wrapper)
	kms := kms.TestKms(t, conn, wrapper)
	repo, err := NewRepository(rw, rw, kms)
	require.NoError(t, err)

	type args struct {
		session *Session
		opt     []Option
	}
	tests := []struct {
		name            string
		args            args
		wantRowsDeleted int
		wantErr         bool
		wantErrMsg      string
	}{
		{
			name: "valid",
			args: args{
				session: TestDefaultSession(t, conn, wrapper, iamRepo),
			},
			wantRowsDeleted: 1,
			wantErr:         false,
		},
		{
			name: "no-public-id",
			args: args{
				session: func() *Session {
					s := AllocSession()
					return &s
				}(),
			},
			wantRowsDeleted: 0,
			wantErr:         true,
			wantErrMsg:      "delete session: missing public id invalid parameter",
		},
		{
			name: "not-found",
			args: args{
				session: func() *Session {
					s := TestDefaultSession(t, conn, wrapper, iamRepo)
					id, err := newId()
					require.NoError(t, err)
					s.PublicId = id
					return s
				}(),
			},
			wantRowsDeleted: 0,
			wantErr:         true,
			wantErrMsg:      "delete session: failed record not found for ",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert := assert.New(t)
			deletedRows, err := repo.DeleteSession(context.Background(), tt.args.session.PublicId, tt.args.opt...)
			if tt.wantErr {
				assert.Error(err)
				assert.Equal(0, deletedRows)
				assert.Contains(err.Error(), tt.wantErrMsg)
				err = db.TestVerifyOplog(t, rw, tt.args.session.PublicId, db.WithOperation(oplog.OpType_OP_TYPE_DELETE), db.WithCreateNotBefore(10*time.Second))
				assert.Error(err)
				assert.True(errors.Is(db.ErrRecordNotFound, err))
				return
			}
			assert.NoError(err)
			assert.Equal(tt.wantRowsDeleted, deletedRows)
			foundSession, _, err := repo.LookupSession(context.Background(), tt.args.session.PublicId)
			assert.NoError(err)
			assert.Nil(foundSession)

			err = db.TestVerifyOplog(t, rw, tt.args.session.PublicId, db.WithOperation(oplog.OpType_OP_TYPE_DELETE), db.WithCreateNotBefore(10*time.Second))
			assert.Error(err)
		})
	}
}