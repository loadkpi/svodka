package telegram

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/gotd/td/tg"
)

// newTestAuth builds a termAuth whose stdin is a pipe preloaded with input.
// The read end of a pipe is not a terminal, so Password takes the bufio
// fallback path (term.ReadPassword needs a real tty and can't be unit-tested).
func newTestAuth(t *testing.T, input string) (*termAuth, *bytes.Buffer) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	if _, err := w.WriteString(input); err != nil {
		t.Fatalf("write input: %v", err)
	}
	_ = w.Close()
	out := &bytes.Buffer{}
	a := newTermAuth(r, out)
	if a.isTTY {
		t.Fatal("pipe read end unexpectedly reported as tty")
	}
	return a, out
}

func TestTermAuthReads(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name   string
		input  string
		call   func(a *termAuth) (string, error)
		want   string
		prompt string
	}{
		{
			name:   "phone trimmed",
			input:  "  +123456789  \n",
			call:   func(a *termAuth) (string, error) { return a.Phone(ctx) },
			want:   "+123456789",
			prompt: "Phone number",
		},
		{
			name:   "code",
			input:  "12345\n",
			call:   func(a *termAuth) (string, error) { return a.Code(ctx, nil) },
			want:   "12345",
			prompt: "Login code",
		},
		{
			name:   "password fallback when not tty",
			input:  "s3cret\n",
			call:   func(a *termAuth) (string, error) { return a.Password(ctx) },
			want:   "s3cret",
			prompt: "2FA password",
		},
		{
			name:  "EOF with no trailing newline",
			input: "+1555",
			call:  func(a *termAuth) (string, error) { return a.Phone(ctx) },
			want:  "+1555",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, out := newTestAuth(t, tt.input)
			got, err := tt.call(a)
			if err != nil {
				t.Fatalf("call: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
			if tt.prompt != "" && !bytes.Contains(out.Bytes(), []byte(tt.prompt)) {
				t.Errorf("prompt %q not written; out = %q", tt.prompt, out.String())
			}
		})
	}
}

// TestTermAuthFlow checks that successive reads consume successive lines from
// the same buffered reader (phone, then code, then password).
func TestTermAuthFlow(t *testing.T) {
	ctx := context.Background()
	a, _ := newTestAuth(t, "+10\ncode99\npass77\n")

	phone, err := a.Phone(ctx)
	if err != nil || phone != "+10" {
		t.Fatalf("Phone = %q, %v", phone, err)
	}
	code, err := a.Code(ctx, nil)
	if err != nil || code != "code99" {
		t.Fatalf("Code = %q, %v", code, err)
	}
	pass, err := a.Password(ctx)
	if err != nil || pass != "pass77" {
		t.Fatalf("Password = %q, %v", pass, err)
	}
}

func TestTermAuthSignUpRefused(t *testing.T) {
	a, _ := newTestAuth(t, "")
	if _, err := a.SignUp(context.Background()); err == nil {
		t.Fatal("SignUp should refuse account creation, got nil error")
	}
}

func TestTermAuthAcceptTOS(t *testing.T) {
	a, _ := newTestAuth(t, "")
	if err := a.AcceptTermsOfService(context.Background(), tg.HelpTermsOfService{}); err != nil {
		t.Fatalf("AcceptTermsOfService: %v", err)
	}
}
