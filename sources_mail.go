package main

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

func sampleMail(query SourceQuery, readers Readers) (map[string]string, error) {
	host := configString(query.Config, "host")
	if host == "" {
		return nil, fmt.Errorf("enter a mail server")
	}
	secret := SourceSecret{}
	if readers.Secret != nil {
		secret = readers.Secret(query.ID)
	}
	if secret.User == "" || secret.Pass == "" {
		return nil, fmt.Errorf("connect a mail account")
	}
	if readers.Mail != nil {
		return readers.Mail(host, secret)
	}
	return imapInbox(host, secret)
}

func imapInbox(host string, secret SourceSecret) (map[string]string, error) {
	if !strings.Contains(host, ":") {
		host += ":993"
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	conn, err := tls.DialWithDialer(dialer, "tcp", host, &tls.Config{MinVersion: tls.VersionTLS12})
	if err != nil {
		return nil, fmt.Errorf("could not reach the mail server")
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	reader := bufio.NewReader(conn)
	if _, err := reader.ReadString('\n'); err != nil {
		return nil, fmt.Errorf("could not reach the mail server")
	}
	if err := imapCmd(conn, reader, "a1", fmt.Sprintf("LOGIN %s %s", imapQuote(secret.User), imapQuote(secret.Pass))); err != nil {
		return nil, fmt.Errorf("the mail account was refused")
	}
	examine, err := imapCmdText(conn, reader, "a2", "EXAMINE INBOX")
	if err != nil {
		return nil, fmt.Errorf("the inbox could not be opened")
	}
	search, err := imapCmdText(conn, reader, "a3", "SEARCH UNSEEN")
	if err != nil {
		return nil, fmt.Errorf("unread mail could not be counted")
	}
	count := imapUnseen(search)
	subject := ""
	if exists := imapExists(examine); exists > 0 {
		fetch, err := imapCmdText(conn, reader, "a4", fmt.Sprintf("FETCH %d BODY.PEEK[HEADER.FIELDS (SUBJECT)]", exists))
		if err == nil {
			subject = imapSubject(fetch)
		}
	}
	_, _ = fmt.Fprintf(conn, "a5 LOGOUT\r\n")
	return map[string]string{"count": strconv.Itoa(count), "subject": subject}, nil
}

func imapQuote(text string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(text) + `"`
}

func imapCmd(conn net.Conn, reader *bufio.Reader, tag, command string) error {
	_, err := imapCmdText(conn, reader, tag, command)
	return err
}

func imapCmdText(conn net.Conn, reader *bufio.Reader, tag, command string) (string, error) {
	if _, err := fmt.Fprintf(conn, "%s %s\r\n", tag, command); err != nil {
		return "", err
	}
	var body strings.Builder
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return body.String(), err
		}
		body.WriteString(line)
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, tag+" ") {
			if strings.Contains(trimmed, " OK") {
				return body.String(), nil
			}
			return body.String(), fmt.Errorf("mail command failed")
		}
	}
}

func imapUnseen(text string) int {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(strings.ToUpper(line), "* SEARCH") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) <= 2 {
			return 0
		}
		return len(fields) - 2
	}
	return 0
}

func imapExists(text string) int {
	exists := 0
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) >= 3 && fields[0] == "*" && strings.EqualFold(fields[2], "EXISTS") {
			n, err := strconv.Atoi(fields[1])
			if err == nil {
				exists = n
			}
		}
	}
	return exists
}

func imapSubject(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if len(line) >= 8 && strings.EqualFold(line[:8], "Subject:") {
			return strings.TrimSpace(line[8:])
		}
	}
	return ""
}
