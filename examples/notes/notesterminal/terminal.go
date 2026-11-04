// Package notesterminal is a line-oriented terminal presentation of the same
// Notes service. It has no dependency on HTML, mobileui, or a web router.
package notesterminal

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
	"unicode"

	"github.com/DonaldMurillo/gofastr-mobile/examples/notes/notes"
)

// Run presents Notes through a reader and writer until quit or EOF, checking
// cancellation between commands. The caller owns unblocking a pending read.
// Commands: list [search], open ID, new, edit ID, quit. New/edit read a title
// followed by body lines terminated by a single period.
func Run(ctx context.Context, service *notes.Service, input io.Reader, output io.Writer) error {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 4096), (1<<20)+1024)
	fmt.Fprintln(output, "NOTES · on this device\nlist [search] | open ID | new | edit ID | quit")
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		fmt.Fprint(output, "notes> ")
		if !scanner.Scan() {
			return scanner.Err()
		}
		command, argument, _ := strings.Cut(strings.TrimSpace(scanner.Text()), " ")
		argument = strings.TrimSpace(argument)
		switch command {
		case "quit", "exit":
			return nil
		case "list":
			items, err := service.List(ctx, argument)
			if err != nil {
				fmt.Fprintln(output, "Error:", printable(err.Error()))
				continue
			}
			for _, note := range items {
				fmt.Fprintf(output, "%s  %s\n", printable(note.ID), printable(note.Title))
			}
			fmt.Fprintf(output, "%d notes\n", len(items))
		case "open":
			note, err := service.Get(ctx, argument)
			if err != nil {
				fmt.Fprintln(output, "Error:", printable(err.Error()))
				continue
			}
			fmt.Fprintf(output, "\n%s\n%s\n\n", printable(note.Title), printable(note.Body))
		case "new", "edit":
			id := ""
			if command == "edit" {
				if _, err := service.Get(ctx, argument); err != nil {
					fmt.Fprintln(output, "Error:", printable(err.Error()))
					continue
				}
				id = argument
			}
			fmt.Fprint(output, "Title: ")
			if !scanner.Scan() {
				return scanner.Err()
			}
			title := scanner.Text()
			fmt.Fprintln(output, "Note (end with a single .):")
			var lines []string
			bodySize := 0
			complete := false
			for scanner.Scan() {
				if scanner.Text() == "." {
					complete = true
					break
				}
				if len(lines) > 0 {
					bodySize++
				}
				bodySize += len(scanner.Text())
				lines = append(lines, scanner.Text())
				if bodySize > 1<<20 {
					return &notes.ValidationError{Message: "This note is too long. Keep it under 1 MB."}
				}
			}
			if !complete {
				fmt.Fprintln(output, "Not saved: incomplete input.")
				return scanner.Err()
			}
			note, err := service.Save(ctx, id, title, strings.Join(lines, "\n"))
			if err != nil {
				fmt.Fprintln(output, "Error:", printable(err.Error()))
				continue
			}
			fmt.Fprintln(output, "Saved on this device:", printable(note.ID))
		case "":
		default:
			fmt.Fprintln(output, "Unknown command. Use list, open, new, edit, or quit.")
		}
	}
}

func printable(text string) string {
	return strings.Map(func(character rune) rune {
		if unicode.IsControl(character) && character != '\n' && character != '\t' {
			return -1
		}
		return character
	}, text)
}
