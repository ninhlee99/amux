package term

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

var sharedStdinReader = bufio.NewReaderSize(os.Stdin, 64*1024)

// ReadLine prints a prompt and reads a single line from os.Stdin.
// It properly handles empty inputs (pressing Enter immediately returns ""),
// trims CR/LF and surrounding whitespace, and supports long pasted tokens.
func ReadLine(prompt string) string {
	if prompt != "" {
		fmt.Print(prompt)
	}
	var sb strings.Builder
	for {
		b, err := sharedStdinReader.ReadByte()
		if err != nil {
			if err == io.EOF && sb.Len() > 0 {
				break
			}
			return strings.TrimSpace(sb.String())
		}
		if b == '\n' || b == '\r' {
			// If CR, peek next for LF and consume if present
			if b == '\r' {
				if next, err := sharedStdinReader.Peek(1); err == nil && len(next) > 0 && next[0] == '\n' {
					_, _ = sharedStdinReader.ReadByte()
				}
			}
			break
		}
		sb.WriteByte(b)
	}
	return strings.TrimSpace(sb.String())
}

// ReadEnterPrompt waits for user to press Enter. Never blocks waiting for non-empty text.
func ReadEnterPrompt(prompt string) {
	_ = ReadLine(prompt)
}
