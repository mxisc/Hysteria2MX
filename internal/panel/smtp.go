package panel

import (
	"bufio"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"time"
)

func sendSMTPTestMail(cfg Config) error {
	if !cfg.SMTPEnabled {
		return errors.New("请先启用 SMTP 通知")
	}
	if strings.TrimSpace(cfg.SMTPHost) == "" || strings.TrimSpace(cfg.SMTPFromEmail) == "" || strings.TrimSpace(cfg.SMTPNotifyEmail) == "" {
		return errors.New("请先完整填写 SMTP 服务器、发件邮箱和接收邮箱")
	}
	return sendSMTPMail(cfg, cfg.SMTPNotifyEmail, "Hysteria2 Panel 通知测试", "这是一封 SMTP 通知测试邮件。\n如果你收到此邮件，说明通知配置已生效。")
}

func sendSMTPMail(cfg Config, to string, subject string, body string) error {
	host := strings.TrimSpace(cfg.SMTPHost)
	port := cfg.SMTPPort
	if port <= 0 {
		port = 587
	}
	encryption := normalizeSMTPEncryption(cfg.SMTPEncryption)
	address := fmt.Sprintf("%s:%d", host, port)

	var conn net.Conn
	var err error
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	if encryption == "ssl" {
		conn, err = tls.DialWithDialer(dialer, "tcp", address, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
	} else {
		conn, err = dialer.Dial("tcp", address)
	}
	if err != nil {
		return errors.New("SMTP 服务连接失败，请检查服务器地址和端口")
	}
	defer conn.Close()

	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)
	if err := expectSMTP(reader, []int{220}); err != nil {
		return err
	}

	hostname := "localhost"
	if name, err := os.Hostname(); err == nil && strings.TrimSpace(name) != "" {
		hostname = name
	}
	if err := smtpCommand(reader, writer, "EHLO "+hostname, []int{250}); err != nil {
		return err
	}

	if encryption == "tls" {
		if err := smtpCommand(reader, writer, "STARTTLS", []int{220}); err != nil {
			return err
		}
		tlsConn := tls.Client(conn, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
		if err := tlsConn.Handshake(); err != nil {
			return errors.New("SMTP TLS 握手失败，请检查加密方式")
		}
		conn = tlsConn
		reader = bufio.NewReader(conn)
		writer = bufio.NewWriter(conn)
		if err := smtpCommand(reader, writer, "EHLO "+hostname, []int{250}); err != nil {
			return err
		}
	}

	if strings.TrimSpace(cfg.SMTPUsername) != "" {
		if err := smtpCommand(reader, writer, "AUTH LOGIN", []int{334}); err != nil {
			return err
		}
		if err := smtpCommand(reader, writer, base64.StdEncoding.EncodeToString([]byte(cfg.SMTPUsername)), []int{334}); err != nil {
			return err
		}
		if err := smtpCommand(reader, writer, base64.StdEncoding.EncodeToString([]byte(cfg.SMTPPassword)), []int{235}); err != nil {
			return err
		}
	}

	if err := smtpCommand(reader, writer, "MAIL FROM:<"+cfg.SMTPFromEmail+">", []int{250}); err != nil {
		return err
	}
	if err := smtpCommand(reader, writer, "RCPT TO:<"+to+">", []int{250, 251}); err != nil {
		return err
	}
	if err := smtpCommand(reader, writer, "DATA", []int{354}); err != nil {
		return err
	}

	fromName := defaultString(strings.TrimSpace(cfg.SMTPFromName), "Hysteria2 Panel")
	headers := []string{
		"From: " + encodedSMTPHeader(fromName) + " <" + cfg.SMTPFromEmail + ">",
		"To: <" + to + ">",
		"Subject: " + encodedSMTPHeader(subject),
		"Date: " + time.Now().Format(time.RFC1123Z),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"Content-Transfer-Encoding: 8bit",
	}
	data := strings.Join(headers, "\r\n") + "\r\n\r\n" + strings.ReplaceAll(body, "\n.", "\n..") + "\r\n.\r\n"
	if _, err := writer.WriteString(data); err != nil {
		return errors.New("SMTP 服务返回异常，请检查通知配置")
	}
	if err := writer.Flush(); err != nil {
		return errors.New("SMTP 服务返回异常，请检查通知配置")
	}
	if err := expectSMTP(reader, []int{250}); err != nil {
		return err
	}
	_ = smtpCommand(reader, writer, "QUIT", []int{221})
	return nil
}

func smtpCommand(reader *bufio.Reader, writer *bufio.Writer, command string, expected []int) error {
	if _, err := writer.WriteString(command + "\r\n"); err != nil {
		return errors.New("SMTP 服务返回异常，请检查通知配置")
	}
	if err := writer.Flush(); err != nil {
		return errors.New("SMTP 服务返回异常，请检查通知配置")
	}
	return expectSMTP(reader, expected)
}

func expectSMTP(reader *bufio.Reader, expected []int) error {
	response := ""
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return errors.New("SMTP 服务返回异常，请检查通知配置")
		}
		response += line
		if len(line) >= 4 && line[3] == ' ' {
			break
		}
	}
	code := 0
	fmt.Sscanf(response[:3], "%d", &code)
	for _, item := range expected {
		if code == item {
			return nil
		}
	}
	if strings.Contains(response, "STARTTLS") {
		return errors.New("SMTP TLS 握手失败，请检查加密方式")
	}
	return errors.New("SMTP 服务返回异常，请检查通知配置")
}

func encodedSMTPHeader(value string) string {
	return "=?UTF-8?B?" + base64.StdEncoding.EncodeToString([]byte(value)) + "?="
}
