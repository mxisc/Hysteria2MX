package panel

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

const sshConnectTimeout = 5 * time.Second

type remoteExecutor struct{}

func newRemoteExecutor() *remoteExecutor {
	return &remoteExecutor{}
}

func (r *remoteExecutor) run(node nodeRecord, command string, displayCommand string) (map[string]any, error) {
	client, err := r.connect(node)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	if strings.TrimSpace(displayCommand) == "" {
		displayCommand = r.buildDisplayCommand(node, command)
	}

	stdout, stderr, exitCode, err := r.executeCommand(client, command)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"command":  r.sanitizeCommand(displayCommand),
		"output":   r.mergeOutput(stdout, stderr),
		"exitCode": exitCode,
	}, nil
}

func (r *remoteExecutor) connect(node nodeRecord) (*ssh.Client, error) {
	host := strings.TrimSpace(node.Host)
	port := node.SSHPort
	username := strings.TrimSpace(node.SSHUsername)
	if username == "" {
		username = "root"
	}

	if host == "" {
		return nil, errors.New("SSH 主机不能为空")
	}
	if port < 1 || port > 65535 {
		return nil, errors.New("SSH 端口无效，请填写 1 到 65535 之间的端口号")
	}
	if err := r.checkPortReachable(host, port); err != nil {
		return nil, err
	}

	authMethod, err := r.buildAuthMethod(node)
	if err != nil {
		return nil, err
	}

	config := &ssh.ClientConfig{
		User:            username,
		Auth:            []ssh.AuthMethod{authMethod},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         sshConnectTimeout,
	}

	client, err := ssh.Dial("tcp", fmt.Sprintf("%s:%d", host, port), config)
	if err != nil {
		return nil, r.normalizeDialError(err)
	}
	return client, nil
}

func (r *remoteExecutor) buildAuthMethod(node nodeRecord) (ssh.AuthMethod, error) {
	if normalizeSSHAuthType(node.SSHAuthType) == "key" {
		keyPath := strings.TrimSpace(node.SSHPrivateKeyPath)
		if keyPath == "" {
			return nil, errors.New("SSH 密钥路径不能为空")
		}
		privateKey, err := os.ReadFile(keyPath)
		if err != nil {
			return nil, errors.New("SSH 私钥文件不存在或不可读")
		}
		signer, err := ssh.ParsePrivateKey(privateKey)
		if err != nil {
			return nil, errors.New("SSH 私钥认证失败")
		}
		return ssh.PublicKeys(signer), nil
	}

	if strings.TrimSpace(node.SSHPassword) == "" {
		return nil, errors.New("SSH 密码不能为空")
	}
	return ssh.Password(node.SSHPassword), nil
}

func (r *remoteExecutor) executeCommand(client *ssh.Client, command string) (string, string, int, error) {
	session, err := client.NewSession()
	if err != nil {
		return "", "", 1, errors.New("远端命令执行失败")
	}
	defer session.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	runErr := session.Run("bash -lc " + shellQuote(command))
	exitCode := 0
	if runErr != nil {
		exitCode = 1
		var exitErr *ssh.ExitError
		if errors.As(runErr, &exitErr) {
			exitCode = exitErr.ExitStatus()
		}
	}

	return strings.TrimSpace(stdout.String()), strings.TrimSpace(stderr.String()), exitCode, nil
}

func (r *remoteExecutor) buildDisplayCommand(node nodeRecord, remoteCommand string) string {
	username := node.SSHUsername
	if strings.TrimSpace(username) == "" {
		username = "root"
	}
	return fmt.Sprintf("ssh2://%s@%s:%d %s", username, node.Host, node.SSHPort, remoteCommand)
}

func (r *remoteExecutor) mergeOutput(stdout string, stderr string) string {
	stdout = strings.TrimSpace(stdout)
	stderr = strings.TrimSpace(stderr)
	switch {
	case stdout != "" && stderr != "":
		return stdout + "\n" + stderr
	case stdout != "":
		return stdout
	default:
		return stderr
	}
}

func (r *remoteExecutor) checkPortReachable(host string, port int) error {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", host, port), sshConnectTimeout)
	if err == nil {
		_ = conn.Close()
		return nil
	}
	return fmt.Errorf("无法连接到 SSH 服务，请确认 SSH 主机和端口可访问（当前端口：%d），并检查服务器防火墙或安全组设置", port)
}

func (r *remoteExecutor) normalizeDialError(err error) error {
	message := strings.ToLower(strings.TrimSpace(err.Error()))
	switch {
	case strings.Contains(message, "unable to authenticate"), strings.Contains(message, "no supported methods remain"):
		return errors.New("SSH 认证失败，请检查节点登录凭据")
	case strings.Contains(message, "handshake failed"), strings.Contains(message, "connection reset"):
		return errors.New("SSH 连接失败：SSH 服务未返回有效握手，请确认填写的是 SSH 端口而不是 Hysteria2 端口，并检查目标主机是否运行 SSH 服务")
	case strings.Contains(message, "timeout"), strings.Contains(message, "i/o timeout"):
		return errors.New("SSH 连接失败：连接超时，请确认主机地址、SSH 端口和网络连通性")
	default:
		return errors.New("SSH 连接失败，请检查 SSH 服务状态、认证信息或服务器安全策略")
	}
}

func (r *remoteExecutor) sanitizeCommand(command string) string {
	command = regexp.MustCompile(`Authorization: ([^'\s]+)`).ReplaceAllString(command, "Authorization: ***")
	command = regexp.MustCompile(`([?&]token=)[^&\s]+`).ReplaceAllString(command, "${1}***")
	return command
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func runLocalCommand(command string, timeout time.Duration) (string, int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "-lc", command)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	output := strings.TrimSpace(stdout.String())
	errText := strings.TrimSpace(stderr.String())
	if errText != "" {
		if output != "" {
			output += "\n"
		}
		output += errText
	}

	exitCode := 0
	if err != nil {
		exitCode = 1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
	}
	return strings.TrimSpace(output), exitCode, err
}
