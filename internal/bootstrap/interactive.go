package bootstrap

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

func Interactive() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

type Console struct {
	in  *bufio.Reader
	out *os.File
}

func NewConsole() Console {
	return Console{in: bufio.NewReader(os.Stdin), out: os.Stdout}
}

func (c Console) Ask(prompt, fallback string) (string, error) {
	if fallback == "" {
		fmt.Fprintf(c.out, "%s：", prompt)
	} else {
		fmt.Fprintf(c.out, "%s [%s]：", prompt, fallback)
	}
	text, err := c.in.ReadString('\n')
	if err != nil {
		return "", err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return fallback, nil
	}
	return text, nil
}

func (c Console) Confirm(prompt string) bool {
	fmt.Fprintf(c.out, "%s [Enter=确认，输入 no 取消]：", prompt)
	text, err := c.in.ReadString('\n')
	if err != nil {
		return false
	}
	text = strings.ToLower(strings.TrimSpace(text))
	return text == "" || text == "y" || text == "yes"
}

func (c Console) Println(args ...any) {
	fmt.Fprintln(c.out, args...)
}
