package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"golang.org/x/term"
)

const adminUsage = `usage:
  portfolio admin set-password [--username <name>] [--password-stdin]

Updates the admin password with an Argon2id hash and invalidates all active sessions.
DATABASE_PATH selects the database (default /data/site.db)`

var adminUsernamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)

func runAdminCommand(args []string) error {
	if len(args) == 0 {
		return errors.New(adminUsage)
	}

	switch args[0] {
	case "set-password":
		return runAdminSetPassword(args[1:])
	default:
		return fmt.Errorf("unknown admin command %q\n\n%s", args[0], adminUsage)
	}
}

func runAdminSetPassword(args []string) error {
	flags := flag.NewFlagSet("admin set-password", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	username := flags.String("username", "admin", "admin username to update")
	passwordStdin := flags.Bool("password-stdin", false, "read the password from stdin instead of prompting")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("%w\n\n%s", err, adminUsage)
	}
	if flags.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s\n\n%s", strings.Join(flags.Args(), " "), adminUsage)
	}
	if !adminUsernamePattern.MatchString(*username) {
		return fmt.Errorf("invalid username %q: use 1-32 letters, digits, dashes or underscores", *username)
	}

	password, err := readAdminPassword(*passwordStdin)
	if err != nil {
		return err
	}

	appConfig = LoadConfig()
	commandDB, err := openDB(appConfig.DatabasePath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() { _ = commandDB.Close() }()

	if err := applyMigrations(commandDB); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}

	if err := setAdminPassword(commandDB, *username, password); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(os.Stdout, "Password updated for admin user %q. All active sessions were invalidated.\n", *username)
	return nil
}

func readAdminPassword(fromStdin bool) (string, error) {
	if fromStdin {
		data, err := io.ReadAll(io.LimitReader(os.Stdin, maxAdminPasswordLength+2))
		if err != nil {
			return "", fmt.Errorf("read password from stdin: %w", err)
		}
		password := strings.TrimRight(string(data), "\r\n")
		if err := validateAdminPassword(password); err != nil {
			return "", err
		}
		return password, nil
	}

	stdinFD := int(os.Stdin.Fd())
	if !term.IsTerminal(stdinFD) {
		return "", errors.New("stdin is not a terminal; use --password-stdin to pipe the password")
	}

	fmt.Fprint(os.Stderr, "New password: ")
	first, err := term.ReadPassword(stdinFD)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	if err := validateAdminPassword(string(first)); err != nil {
		return "", err
	}

	fmt.Fprint(os.Stderr, "Confirm password: ")
	second, err := term.ReadPassword(stdinFD)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("read password confirmation: %w", err)
	}
	if string(first) != string(second) {
		return "", errors.New("passwords do not match")
	}

	return string(first), nil
}
