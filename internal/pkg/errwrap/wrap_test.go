// SPDX-License-Identifier: Apache-2.0

package errwrap

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWrap_NilError(t *testing.T) {
	t.Parallel()
	result := Wrap(nil, "context")
	require.Nil(t, result)
}

func TestWrap_NonNilError(t *testing.T) {
	t.Parallel()
	original := errors.New("original error")
	result := Wrap(original, "context")
	require.ErrorContains(t, result, "context")
	require.ErrorContains(t, result, "original error")
	require.ErrorIs(t, result, original)
}

func TestWrapf_NilError(t *testing.T) {
	t.Parallel()
	result := Wrapf(nil, "context %s", "arg")
	require.Nil(t, result)
}

func TestWrapf_NonNilError(t *testing.T) {
	t.Parallel()
	original := errors.New("original")
	result := Wrapf(original, "failed to %s", "process")
	require.ErrorContains(t, result, "failed to process")
	require.ErrorIs(t, result, original)
}

func TestIs(t *testing.T) {
	t.Parallel()
	target := errors.New("target")
	wrapped := Wrap(target, "wrapper")
	require.True(t, Is(wrapped, target))
	require.False(t, Is(wrapped, errors.New("other")))
}

type customError struct {
	msg string
}

func (e *customError) Error() string { return e.msg }

func TestAs(t *testing.T) {
	t.Parallel()
	var target *customError
	original := &customError{msg: "test"} // ptrext:allow test struct
	wrapped := Wrap(original, "wrapper")
	require.True(t, As(wrapped, &target)) // ptrext:allow errors.As out-param
	require.Equal(t, "test", target.msg)
}

func TestJoin(t *testing.T) {
	t.Parallel()
	e1 := errors.New("error1")
	e2 := errors.New("error2")
	joined := Join(e1, e2)
	require.ErrorIs(t, joined, e1)
	require.ErrorIs(t, joined, e2)
}

func TestNew(t *testing.T) {
	t.Parallel()
	err := New("test error")
	require.EqualError(t, err, "test error")
}

func TestNewf(t *testing.T) {
	t.Parallel()
	err := Newf("error %d: %s", 42, "test")
	require.EqualError(t, err, "error 42: test")
}

func TestIfErr_Nil(t *testing.T) {
	t.Parallel()
	result := IfErr(nil, "context")
	require.Nil(t, result)
}

func TestIfErr_NonNil(t *testing.T) {
	t.Parallel()
	original := errors.New("original")
	result := IfErr(original, "context")
	require.ErrorContains(t, result, "context")
	require.ErrorIs(t, result, original)
}

func TestMust_Success(t *testing.T) {
	t.Parallel()
	result := Must(42, nil)
	require.Equal(t, 42, result)
}

func TestMust_Panic(t *testing.T) {
	t.Parallel()
	require.Panics(t, func() {
		Must(0, errors.New("error"))
	})
}
