package service

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParallelOk(t *testing.T) {
	x := [10]int{}
	err := Parallel(func(s ParallelScope) error {
		for i := range x {
			s.Spawn(func() error {
				x[i] = i
				return nil
			})
		}
		return nil
	})
	require.NoError(t, err)
	for want, got := range x {
		require.Equal(t, want, got, "x[%d] = %d, want %d", want, got, want)
	}
}

func TestParallelFail(t *testing.T) {
	var wantErr = errors.New("custom err")
	x := [10]int{}
	err := Parallel(func(s ParallelScope) error {
		for i := range x {
			s.Spawn(func() error {
				if i%2 == 0 {
					return wantErr
				}
				x[i] = i
				return nil
			})
		}
		return nil
	})
	require.ErrorIs(t, wantErr, err, "err = %v, want %v", err, wantErr)
	for want, got := range x {
		if want%2 == 0 {
			want = 0
		}
		require.Equal(t, want, got, "x[%d] = %d, want %d", want, got, want)
	}
}
