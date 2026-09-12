package rest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLongConnRegistry(t *testing.T) {
	require.Equal(t, 0, ActiveLongConns())

	ctx1, cancel1 := context.WithCancel(context.Background())
	ctx2, cancel2 := context.WithCancel(context.Background())
	un1 := RegisterLongConn(cancel1)
	un2 := RegisterLongConn(cancel2)
	require.Equal(t, 2, ActiveLongConns())

	CancelLongConns()
	require.Equal(t, 0, ActiveLongConns())
	require.Error(t, ctx1.Err())
	require.Error(t, ctx2.Err())

	// Unregistering after the registry was cleared is harmless.
	un1()
	un2()

	// New registrations after shutdown began are cancelled immediately
	// instead of being stored (they would otherwise outlive shutdown).
	ctx3, cancel3 := context.WithCancel(context.Background())
	un3 := RegisterLongConn(cancel3)
	require.Equal(t, 0, ActiveLongConns())
	require.Error(t, ctx3.Err())
	un3() // no-op unregister
	require.Equal(t, 0, ActiveLongConns())
}
