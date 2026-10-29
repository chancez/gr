package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

// bodyFlags reads a comment body from -m, -F, stdin, or $EDITOR, in that order.
type bodyFlags struct {
	message string
	file    string
}

func (f *bodyFlags) register(cmd *cobra.Command) {
	cmd.Flags().StringVarP(&f.message, "message", "m", "", "body text (Markdown)")
	cmd.Flags().StringVarP(&f.file, "file", "F", "", `read the body from a file, "-" for stdin`)
}

func (f *bodyFlags) given() bool { return f.message != "" || f.file != "" }

// read returns the body. With no flag it opens $EDITOR, but only on a terminal: stdin is never read
// implicitly, because an agent whose stdin is an open pipe would hang.
func (f *bodyFlags) read(a *app, initial string) (string, error) {
	switch {
	case f.message != "":
		return f.message, nil
	case f.file == "-":
		return readAll(a.stdin)
	case f.file != "":
		b, err := os.ReadFile(f.file)
		return string(b), err
	}
	if file, ok := a.stdin.(*os.File); !ok || !isTerminal(file) {
		return "", errors.New("no body: pass -m, or -F with a file or - for stdin")
	}
	return EditText(initial + "\n\n" + Scissors + "\n# Write the comment above. Everything from the line above down is dropped.\n")
}

func readAll(r io.Reader) (string, error) {
	b, err := io.ReadAll(r)
	return string(b), err
}

func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// Scissors marks the end of the body in an editor buffer. It and everything after it are dropped,
// like git's commit.cleanup=scissors, so Markdown headings starting with "#" survive.
const Scissors = "# ------------------------ >8 ------------------------"

// EditText opens $VISUAL or $EDITOR on initial and returns the result, cut at Scissors.
func EditText(initial string) (string, error) {
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}
	tmp, err := os.CreateTemp("", "gr-*.md")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(initial); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	// Through the shell, so EDITOR may carry arguments, as git allows.
	cmd := exec.Command("sh", "-c", editor+` "$1"`, "sh", tmp.Name())
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("editor %q: %w", editor, err)
	}
	b, err := os.ReadFile(tmp.Name())
	if err != nil {
		return "", err
	}
	text := string(b)
	if i := strings.Index(text, Scissors); i >= 0 {
		text = text[:i]
	}
	body := strings.TrimSpace(text)
	if body == "" {
		return "", errors.New("empty body, aborting")
	}
	return body, nil
}
