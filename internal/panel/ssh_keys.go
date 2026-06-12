package panel

import (
	"crypto/rsa"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

const (
	maxSSHKeyFileSize = 262144
	sshKeyTempTTL     = 24 * time.Hour
)

type sshKeyUploadService struct {
	rootDir string
}

func newSSHKeyUploadService(cfg Config) *sshKeyUploadService {
	return &sshKeyUploadService{rootDir: projectRootFromConfig(cfg)}
}

func (s *sshKeyUploadService) upload(privateFile multipart.File, header *multipart.FileHeader) (map[string]any, error) {
	if privateFile == nil || header == nil {
		return nil, errors.New("请选择 SSH 私钥文件")
	}
	defer privateFile.Close()

	if err := s.pruneStaleTemporaryFiles(); err != nil {
		return nil, err
	}

	privateName := filepath.Base(strings.TrimSpace(header.Filename))
	if privateName == "" {
		return nil, errors.New("SSH 私钥文件名不能为空")
	}
	if header.Size <= 0 || header.Size > maxSSHKeyFileSize {
		return nil, errors.New("SSH 私钥大小不合法，请上传 1B 到 256KB 之间的文件")
	}

	privateContents, err := io.ReadAll(io.LimitReader(privateFile, maxSSHKeyFileSize+1))
	if err != nil {
		return nil, errors.New("SSH 私钥读取失败，请重新上传")
	}
	if len(privateContents) == 0 || len(privateContents) > maxSSHKeyFileSize {
		return nil, errors.New("SSH 私钥大小不合法，请上传 1B 到 256KB 之间的文件")
	}

	if err := assertPrivateKeyContents(privateContents); err != nil {
		return nil, err
	}

	publicName, publicContents, err := derivePublicKey(privateContents, privateName)
	if err != nil {
		return nil, err
	}

	token, err := randomHex(16)
	if err != nil {
		return nil, errors.New("SSH 密钥上传标识生成失败，请稍后重试")
	}

	tempPrivatePath := filepath.Join(s.temporaryDirectory(), token)
	tempPublicPath := tempPrivatePath + ".pub"
	tempMetadataPath := tempPrivatePath + ".json"

	metadataBytes, err := json.Marshal(map[string]any{
		"private_key_name": privateName,
		"public_key_name":  publicName,
		"created_at":       time.Now().Unix(),
	})
	if err != nil {
		return nil, errors.New("SSH 密钥元数据生成失败，请稍后重试")
	}

	if err := os.WriteFile(tempPrivatePath, privateContents, 0o600); err != nil {
		return nil, errors.New("SSH 私钥保存失败，请稍后重试")
	}
	if err := os.WriteFile(tempPublicPath, publicContents, 0o644); err != nil {
		return nil, errors.New("SSH 公钥保存失败，请稍后重试")
	}
	if err := os.WriteFile(tempMetadataPath, metadataBytes, 0o600); err != nil {
		return nil, errors.New("SSH 密钥元数据保存失败，请稍后重试")
	}

	return map[string]any{
		"token":            token,
		"private_key_name": privateName,
		"public_key_name":  publicName,
	}, nil
}

func (s *sshKeyUploadService) commit(token string, currentPrivateKeyPath string) (string, error) {
	if err := s.pruneStaleTemporaryFiles(); err != nil {
		return "", err
	}
	token = strings.TrimSpace(token)
	if !isHexToken(token, 32) {
		return "", errors.New("SSH 密钥上传标识无效，请重新上传")
	}

	tempPrivatePath := filepath.Join(s.temporaryDirectory(), token)
	tempPublicPath := tempPrivatePath + ".pub"
	tempMetadataPath := tempPrivatePath + ".json"
	if !isRegularFile(tempPrivatePath) || !isRegularFile(tempPublicPath) {
		return "", errors.New("SSH 密钥上传已失效，请重新上传后再保存")
	}

	finalToken, err := randomHex(16)
	if err != nil {
		return "", errors.New("SSH 密钥保存失败，请稍后重试")
	}

	finalPrivatePath := filepath.Join(s.managedDirectory(), finalToken)
	finalPublicPath := finalPrivatePath + ".pub"
	finalMetadataPath := finalPrivatePath + ".json"

	if err := os.Rename(tempPrivatePath, finalPrivatePath); err != nil {
		return "", errors.New("SSH 私钥保存失败，请稍后重试")
	}
	if err := os.Rename(tempPublicPath, finalPublicPath); err != nil {
		return "", errors.New("SSH 公钥保存失败，请稍后重试")
	}
	if isRegularFile(tempMetadataPath) {
		if err := os.Rename(tempMetadataPath, finalMetadataPath); err != nil {
			return "", errors.New("SSH 密钥元数据保存失败，请稍后重试")
		}
	}

	_ = os.Chmod(finalPrivatePath, 0o600)
	_ = os.Chmod(finalPublicPath, 0o644)
	_ = os.Chmod(finalMetadataPath, 0o600)

	if strings.TrimSpace(currentPrivateKeyPath) != "" {
		s.deleteManagedKey(currentPrivateKeyPath, finalPrivatePath)
	}

	return finalPrivatePath, nil
}

func (s *sshKeyUploadService) describe(privateKeyPath string) map[string]any {
	privateKeyPath = strings.TrimSpace(privateKeyPath)
	if privateKeyPath == "" {
		return map[string]any{
			"uploaded":         false,
			"private_key_name": nil,
			"public_key_name":  nil,
		}
	}

	if !s.isManagedPrivateKeyPath(privateKeyPath) {
		return map[string]any{
			"uploaded":         true,
			"private_key_name": "已配置服务器密钥",
			"public_key_name":  nil,
		}
	}

	metadata := s.loadMetadata(privateKeyPath)
	return map[string]any{
		"uploaded":         true,
		"private_key_name": metadata["private_key_name"],
		"public_key_name":  metadata["public_key_name"],
	}
}

func (s *sshKeyUploadService) deleteManagedKey(privateKeyPath string, exceptPrivatePath string) {
	privateKeyPath = strings.TrimSpace(privateKeyPath)
	if privateKeyPath == "" || !s.isManagedPrivateKeyPath(privateKeyPath) {
		return
	}

	paths := []string{
		privateKeyPath,
		privateKeyPath + ".pub",
		privateKeyPath + ".json",
	}

	for _, path := range paths {
		if exceptPrivatePath != "" && (path == exceptPrivatePath || path == exceptPrivatePath+".pub" || path == exceptPrivatePath+".json") {
			continue
		}
		if isRegularFile(path) {
			_ = os.Remove(path)
		}
	}
}

func (s *sshKeyUploadService) isManagedPrivateKeyPath(privateKeyPath string) bool {
	privateKeyPath = strings.TrimSpace(privateKeyPath)
	if privateKeyPath == "" {
		return false
	}

	managedDirectory, err := filepath.Abs(s.managedDirectory())
	if err != nil {
		return false
	}
	realPath, err := filepath.Abs(privateKeyPath)
	if err != nil {
		return false
	}

	return strings.HasPrefix(realPath, managedDirectory+string(os.PathSeparator))
}

func (s *sshKeyUploadService) pruneStaleTemporaryFiles() error {
	directory := s.temporaryDirectory()
	entries, err := os.ReadDir(directory)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return errors.New("SSH 密钥临时目录读取失败，请检查运行环境权限")
	}

	expireBefore := time.Now().Add(-sshKeyTempTTL)
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil || info.ModTime().After(expireBefore) {
			continue
		}
		_ = os.Remove(filepath.Join(directory, entry.Name()))
	}
	return nil
}

func (s *sshKeyUploadService) temporaryDirectory() string {
	path := filepath.Join(s.rootDir, "storage", "ssh-keys", "tmp")
	_ = os.MkdirAll(path, 0o700)
	_ = os.Chmod(path, 0o700)
	return path
}

func (s *sshKeyUploadService) managedDirectory() string {
	path := filepath.Join(s.rootDir, "storage", "ssh-keys")
	_ = os.MkdirAll(path, 0o700)
	_ = os.Chmod(path, 0o700)
	return path
}

func (s *sshKeyUploadService) loadMetadata(privateKeyPath string) map[string]any {
	contents, err := os.ReadFile(privateKeyPath + ".json")
	if err != nil || len(contents) == 0 {
		return map[string]any{
			"private_key_name": "已上传 SSH 私钥",
			"public_key_name":  nil,
		}
	}

	var data map[string]any
	if err := json.Unmarshal(contents, &data); err != nil {
		return map[string]any{
			"private_key_name": "已上传 SSH 私钥",
			"public_key_name":  nil,
		}
	}

	if _, ok := data["private_key_name"]; !ok {
		data["private_key_name"] = "已上传 SSH 私钥"
	}
	return data
}

func assertPrivateKeyContents(contents []byte) error {
	normalized := strings.ReplaceAll(strings.TrimSpace(string(contents)), "\r\n", "\n")
	allowedHeaders := []string{
		"-----BEGIN RSA PRIVATE KEY-----",
		"-----BEGIN EC PRIVATE KEY-----",
		"-----BEGIN PRIVATE KEY-----",
		"-----BEGIN DSA PRIVATE KEY-----",
		"-----BEGIN OPENSSH PRIVATE KEY-----",
	}

	for _, header := range allowedHeaders {
		if strings.HasPrefix(normalized, header) {
			return nil
		}
	}

	return errors.New("SSH 私钥格式不受支持，请上传 PEM、PKCS8 或 OpenSSH 格式的私钥文件")
}

func derivePublicKey(privateContents []byte, privateName string) (string, []byte, error) {
	rawKey, err := ssh.ParseRawPrivateKey(privateContents)
	if err != nil {
		return "", nil, errors.New("SSH 私钥解析失败，请检查文件格式是否完整")
	}

	var publicKey ssh.PublicKey
	switch key := rawKey.(type) {
	case *rsa.PrivateKey:
		publicKey, err = ssh.NewPublicKey(&key.PublicKey)
	default:
		return "", nil, errors.New("当前仅支持 RSA 私钥自动派生 SSH 公钥")
	}
	if err != nil {
		return "", nil, errors.New("SSH 公钥派生失败，请检查私钥格式是否完整")
	}

	return filepath.Base(privateName) + ".pub", ssh.MarshalAuthorizedKey(publicKey), nil
}

func projectRootFromConfig(cfg Config) string {
	return cfg.ProjectRoot()
}

func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func isHexToken(value string, length int) bool {
	if len(value) != length {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
