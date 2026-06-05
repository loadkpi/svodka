package telegram

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"
	"golang.org/x/term"
)

// termAuth implements auth.UserAuthenticator by prompting on a terminal. It is
// used by the login command to perform the interactive sign-in that produces a
// session. Prompts go to out (stderr by convention) so stdout stays clean; the
// entered phone/code/password are never logged.
type termAuth struct {
	in    *bufio.Reader
	out   io.Writer
	inFd  int  // file descriptor of the underlying stdin, for term.ReadPassword
	isTTY bool // whether inFd is an interactive terminal
}

// TermAuth returns an interactive auth.UserAuthenticator that reads phone, code
// and 2FA password from in and writes prompts to out. The login command passes
// os.Stdin and os.Stderr. It returns the interface (not the concrete type) to
// match gotd's own auth.Constant/CodeOnly/Env constructors; the type carries no
// API beyond the interface.
func TermAuth(in *os.File, out io.Writer) auth.UserAuthenticator {
	return newTermAuth(in, out)
}

// newTermAuth builds a termAuth reading lines from in and writing prompts to
// out. When in is the real *os.Stdin on a terminal, the 2FA password is read
// without echo; otherwise it falls back to a plain line read.
func newTermAuth(in *os.File, out io.Writer) *termAuth {
	fd := int(in.Fd())
	return &termAuth{
		in:    bufio.NewReader(in),
		out:   out,
		inFd:  fd,
		isTTY: term.IsTerminal(fd),
	}
}

// readLine prints a prompt and reads a single trimmed line from stdin.
func (a *termAuth) readLine(prompt string) (string, error) {
	fmt.Fprint(a.out, prompt)
	line, err := a.in.ReadString('\n')
	if err != nil {
		// io.EOF with a partial line still gives us what was typed.
		if errors.Is(err, io.EOF) && line != "" {
			return strings.TrimSpace(line), nil
		}
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func (a *termAuth) Phone(_ context.Context) (string, error) {
	return a.readLine("Phone number (international format, e.g. +123456789): ")
}

func (a *termAuth) Code(_ context.Context, _ *tg.AuthSentCode) (string, error) {
	return a.readLine("Login code (from Telegram): ")
}

// Password is called only when the account has 2FA enabled. On an interactive
// terminal it reads without echo; otherwise it falls back to a plain line.
func (a *termAuth) Password(_ context.Context) (string, error) {
	if !a.isTTY {
		return a.readLine("2FA password: ")
	}
	fmt.Fprint(a.out, "2FA password (input hidden): ")
	b, err := term.ReadPassword(a.inFd)
	fmt.Fprintln(a.out)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

// AcceptTermsOfService is reached only during sign up, which we refuse below;
// accepting here is harmless and keeps the interface satisfied.
func (a *termAuth) AcceptTermsOfService(_ context.Context, _ tg.HelpTermsOfService) error {
	return nil
}

// SignUp is invoked when the phone number has no Telegram account. svodka logs
// into an existing account only, so we refuse and tell the user what to do.
func (a *termAuth) SignUp(_ context.Context) (auth.UserInfo, error) {
	return auth.UserInfo{}, errors.New("this phone number has no Telegram account; create one in an official Telegram client first")
}
