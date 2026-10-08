package extraheader

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/silog"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		spec    string
		want    Spec
		wantErr string
	}{
		{
			name: "Static",
			spec: "Foo: bar",
			want: Spec{Name: "Foo", Value: "bar"},
		},
		{
			name: "CanonicalizesName",
			spec: "foo-bar: baz qux",
			want: Spec{Name: "Foo-Bar", Value: "baz qux"},
		},
		{
			name: "NoSpaceAfterColon",
			spec: "Foo:bar",
			want: Spec{Name: "Foo", Value: "bar"},
		},
		{
			name: "Command",
			spec: "Foo: !echo hello world",
			want: Spec{Name: "Foo", Command: []string{"echo", "hello", "world"}},
		},
		{
			name: "CommandQuoted",
			spec: "Foo: !sh -c 'exit 1'",
			want: Spec{Name: "Foo", Command: []string{"sh", "-c", "exit 1"}},
		},
		{
			name:    "MissingColon",
			spec:    "Foo",
			wantErr: "missing ':'",
		},
		{
			name:    "EmptyName",
			spec:    ": value",
			wantErr: "empty header name",
		},
		{
			name:    "EmptyValue",
			spec:    "Foo:",
			wantErr: "empty header value",
		},
		{
			name:    "InvalidName",
			spec:    "Foo Bar: value",
			wantErr: "invalid header name",
		},
		{
			name:    "InvalidCommand",
			spec:    "Foo: !echo 'unclosed",
			wantErr: "parse header",
		},
		{
			name:    "EmptyCommand",
			spec:    "Foo: !  ",
			wantErr: "empty command",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.spec)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.ErrorContains(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestResolve(t *testing.T) {
	t.Run("StaticAndCommand", func(t *testing.T) {
		ctx := t.Context()
		var gotCmd []string
		run := func(_ context.Context, name string, args []string) (string, error) {
			gotCmd = append([]string{name}, args...)
			return " tok \n", nil
		}

		headers, err := Resolve(ctx,
			[]string{"X-A: one", "X-B: !cmd arg"},
			run, silog.Nop())
		require.NoError(t, err)

		assert.Equal(t, []Header{
			{Name: "X-A", Value: "one"},
			{Name: "X-B", Value: "tok"},
		}, headers)
		assert.Equal(t, []string{"cmd", "arg"}, gotCmd)
	})

	t.Run("CommandFails", func(t *testing.T) {
		run := func(context.Context, string, []string) (string, error) {
			return "", errors.New("great sadness")
		}

		_, err := Resolve(t.Context(),
			[]string{"X-A: !cmd arg"}, run, silog.Nop())
		require.Error(t, err)
		assert.ErrorContains(t, err, "X-A: !cmd arg")
		assert.ErrorContains(t, err, "great sadness")
	})

	t.Run("CommandMultiLineOutput", func(t *testing.T) {
		run := func(context.Context, string, []string) (string, error) {
			return "tok\nextra", nil
		}

		_, err := Resolve(t.Context(),
			[]string{"X-A: !cmd"}, run, silog.Nop())
		require.Error(t, err)
		assert.ErrorContains(t, err, "single line")
	})

	t.Run("CommandEmptyOutput", func(t *testing.T) {
		run := func(context.Context, string, []string) (string, error) {
			return "  \n", nil
		}

		_, err := Resolve(t.Context(),
			[]string{"X-A: !cmd"}, run, silog.Nop())
		require.Error(t, err)
		assert.ErrorContains(t, err, "empty")
	})

	t.Run("DuplicateNames", func(t *testing.T) {
		_, err := Resolve(t.Context(),
			[]string{"X-A: one", "x-a: two"},
			nil, silog.Nop())
		require.Error(t, err)
		assert.ErrorContains(t, err, "duplicate header")
	})

	t.Run("InvalidSpec", func(t *testing.T) {
		_, err := Resolve(t.Context(),
			[]string{"X-A"}, nil, silog.Nop())
		require.Error(t, err)
		assert.ErrorContains(t, err, "missing ':'")
	})

	t.Run("NilRunnerWithCommand", func(t *testing.T) {
		_, err := Resolve(t.Context(),
			[]string{"X-A: !cmd"}, nil, silog.Nop())
		require.Error(t, err)
		assert.ErrorContains(t, err, "X-A")
	})

	t.Run("NilRunnerStaticOnly", func(t *testing.T) {
		headers, err := Resolve(t.Context(),
			[]string{"X-A: one"}, nil, silog.Nop())
		require.NoError(t, err)
		assert.Equal(t, []Header{{Name: "X-A", Value: "one"}}, headers)
	})
}

func TestTransport_RoundTrip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(r.Header.Get("X-Test")))
	}))
	defer srv.Close()

	t.Run("AttachesHeaders", func(t *testing.T) {
		client := Client(nil, []Header{{Name: "X-Test", Value: "hello"}})

		res, err := client.Get(srv.URL)
		require.NoError(t, err)
		defer func() { assert.NoError(t, res.Body.Close()) }()

		body := new(strings.Builder)
		_, err = io.Copy(body, res.Body)
		require.NoError(t, err)
		assert.Equal(t, "hello", body.String())
	})

	t.Run("OverridesRequestHeaders", func(t *testing.T) {
		client := Client(nil, []Header{{Name: "X-Test", Value: "new"}})

		req, err := http.NewRequestWithContext(
			t.Context(), http.MethodGet, srv.URL, nil)
		require.NoError(t, err)
		req.Header.Set("X-Test", "old")

		res, err := client.Do(req)
		require.NoError(t, err)
		defer func() { assert.NoError(t, res.Body.Close()) }()

		body := new(strings.Builder)
		_, err = io.Copy(body, res.Body)
		require.NoError(t, err)
		assert.Equal(t, "new", body.String())
	})
}

func TestClient(t *testing.T) {
	t.Run("NoHeaders", func(t *testing.T) {
		base := &http.Client{}
		assert.Same(t, base, Client(base, nil))
	})

	t.Run("WrapsTransport", func(t *testing.T) {
		baseTransport := &http.Transport{}
		base := &http.Client{Transport: baseTransport, Timeout: 5}

		got := Client(base, []Header{{Name: "X-Test", Value: "hello"}})
		require.NotNil(t, got)

		transport, ok := got.Transport.(*Transport)
		require.True(t, ok, "expected *Transport, got %T", got.Transport)
		assert.Same(t, baseTransport, transport.Base)
		assert.Equal(t, base.Timeout, got.Timeout)
	})

	t.Run("NilBaseUsesDefaultTransport", func(t *testing.T) {
		got := Client(nil, []Header{{Name: "X-Test", Value: "hello"}})
		require.NotNil(t, got)

		transport, ok := got.Transport.(*Transport)
		require.True(t, ok, "expected *Transport, got %T", got.Transport)
		assert.Equal(t, http.DefaultTransport, transport.Base)

		// The default client must not be modified.
		assert.Nil(t, http.DefaultClient.Transport)
	})
}
