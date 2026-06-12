package panel

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const gitlabUpstreamProjectID = "82842050"
const gitlabRuntimePackageName = "mxinhy-runtime"
const gitlabRuntimePackageFileName = "hysteria2-plan.tar.gz"
const defaultPanelServiceName = "mxinhy-panel"
const defaultUpgradeKeepReleases = 5

type installLayout struct {
	mode              string
	rootDir           string
	deployRoot        string
	releasesDir       string
	currentLink       string
	sharedDir         string
	currentReleaseDir string
	panelEnvPath      string
	panelServiceName  string
	backupRoot        string
}

func checkLatestVersion(rootDir string) (map[string]any, error) {
	currentVersion := currentAppVersion(rootDir)
	latestVersion := ""
	latestTag := ""

	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest(http.MethodGet, "https://gitlab.com/api/v4/projects/"+gitlabUpstreamProjectID+"/repository/tags?per_page=1&order_by=updated", nil)
	if err != nil {
		return nil, errors.New("检查更新失败")
	}
	req.Header.Set("User-Agent", "mxinhy-version-check")
	resp, err := client.Do(req)
	if err != nil {
		return map[string]any{
			"current":   currentVersion,
			"latest":    nil,
			"hasUpdate": false,
			"error":     "检查更新失败",
		}, nil
	}
	defer resp.Body.Close()
	var tags []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&tags); err == nil && len(tags) > 0 {
		latestTag = toString(tags[0]["name"])
		latestVersion = strings.TrimPrefix(latestTag, "v")
	}
	hasUpdate := latestVersion != "" && currentVersion != "unknown" && compareVersion(latestVersion, currentVersion) > 0
	var latest any
	if latestVersion != "" {
		latest = latestVersion
	}
	return map[string]any{
		"current":   currentVersion,
		"latest":    latest,
		"hasUpdate": hasUpdate,
		"error":     nil,
	}, nil
}

func upgradeLatestVersion(rootDir string) (map[string]any, error) {
	layout := detectInstallLayout(rootDir)
	check, err := checkLatestVersion(rootDir)
	if err != nil {
		return nil, err
	}
	current := toString(check["current"])
	latest, _ := check["latest"].(string)
	if latest == "" {
		return nil, errors.New("无法获取最新版本信息")
	}
	if compareVersion(latest, current) <= 0 {
		return nil, errors.New("当前已是最新版本")
	}

	backupDir := filepath.Join(layout.backupRoot, "backup_"+time.Now().Format("20060102_150405"))
	if err := backupRuntimeState(layout, backupDir); err != nil {
		return nil, errors.New("创建备份失败")
	}

	tmpDir, err := os.MkdirTemp("", "mxinhy-upgrade-*")
	if err != nil {
		return nil, errors.New("创建临时目录失败")
	}
	defer os.RemoveAll(tmpDir)
	archivePath := filepath.Join(tmpDir, gitlabRuntimePackageFileName)
	extractDir := filepath.Join(tmpDir, "extracted")
	tag := "v" + latest
	if err := downloadToFile(runtimePackageURL(tag), archivePath, "mxinhy-upgrade"); err != nil {
		return nil, errors.New("下载更新包失败")
	}
	if err := untarGz(archivePath, extractDir); err != nil {
		return nil, errors.New("解压更新包失败")
	}

	sourceDir, err := resolveRuntimePackageRoot(extractDir)
	if err != nil {
		return nil, errors.New("更新包内容无效")
	}
	if err := applyRuntimeUpgrade(layout, sourceDir, latest); err != nil {
		return nil, errors.New("更新文件失败")
	}

	return map[string]any{
		"message": "升级成功",
		"from":    current,
		"to":      latest,
		"backup":  filepath.Base(backupDir),
	}, nil
}

func currentAppVersion(rootDir string) string {
	content, err := os.ReadFile(filepath.Join(rootDir, "package.json"))
	if err != nil {
		return "unknown"
	}
	var pkg map[string]any
	if err := json.Unmarshal(content, &pkg); err != nil {
		return "unknown"
	}
	version := toString(pkg["version"])
	if version == "" {
		return "unknown"
	}
	return version
}

func runtimePackageURL(tag string) string {
	return "https://gitlab.com/api/v4/projects/" + gitlabUpstreamProjectID + "/packages/generic/" + gitlabRuntimePackageName + "/" + tag + "/" + gitlabRuntimePackageFileName
}

func detectInstallLayout(rootDir string) installLayout {
	cleanRoot := filepath.Clean(rootDir)
	layout := installLayout{
		mode:             "fixed",
		rootDir:          cleanRoot,
		deployRoot:       cleanRoot,
		panelEnvPath:     filepath.Join(cleanRoot, "config", "panel.env"),
		panelServiceName: defaultPanelServiceName,
		backupRoot:       cleanRoot,
	}
	if filepath.Base(filepath.Dir(cleanRoot)) == "releases" {
		deployRoot := filepath.Dir(filepath.Dir(cleanRoot))
		layout.mode = "releases"
		layout.deployRoot = deployRoot
		layout.releasesDir = filepath.Join(deployRoot, "releases")
		layout.currentLink = filepath.Join(deployRoot, "current")
		layout.sharedDir = filepath.Join(deployRoot, "shared")
		layout.currentReleaseDir = cleanRoot
		layout.panelEnvPath = filepath.Join(layout.sharedDir, "config", "panel.env")
		layout.backupRoot = deployRoot
	}
	return layout
}

func backupRuntimeState(layout installLayout, backupDir string) error {
	var configPath string
	var storagePath string
	if layout.mode == "releases" {
		configPath = filepath.Join(layout.sharedDir, "config")
		storagePath = filepath.Join(layout.sharedDir, "storage")
	} else {
		configPath = filepath.Join(layout.rootDir, "config")
		storagePath = filepath.Join(layout.rootDir, "storage")
	}
	if err := copyDir(configPath, filepath.Join(backupDir, "config")); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := copyDir(storagePath, filepath.Join(backupDir, "storage")); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func resolveRuntimePackageRoot(extractDir string) (string, error) {
	if isRuntimePackageRoot(extractDir) {
		return extractDir, nil
	}
	child, err := firstChildDirectory(extractDir)
	if err != nil {
		return "", err
	}
	if isRuntimePackageRoot(child) {
		return child, nil
	}
	return "", errors.New("runtime package root not found")
}

func isRuntimePackageRoot(path string) bool {
	if info, err := os.Stat(filepath.Join(path, "build", "panel", "mxinhy-panel")); err != nil || info.IsDir() {
		return false
	}
	if info, err := os.Stat(filepath.Join(path, "config", "panel.env.example")); err != nil || info.IsDir() {
		return false
	}
	if info, err := os.Stat(filepath.Join(path, "deploy", "systemd", "mxinhy-panel.service")); err != nil || info.IsDir() {
		return false
	}
	return true
}

func applyRuntimeUpgrade(layout installLayout, sourceDir string, version string) error {
	if layout.mode == "releases" {
		return upgradeReleaseInstall(layout, sourceDir, version)
	}
	return upgradeFixedInstall(layout, sourceDir)
}

func upgradeFixedInstall(layout installLayout, sourceDir string) error {
	if err := replaceReleaseTree(sourceDir, layout.rootDir); err != nil {
		return err
	}
	return restartPanelService(layout.panelServiceName)
}

func upgradeReleaseInstall(layout installLayout, sourceDir string, version string) error {
	if err := os.MkdirAll(layout.releasesDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(layout.sharedDir, "config"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(layout.sharedDir, "storage"), 0o755); err != nil {
		return err
	}
	releaseName := strings.ReplaceAll(version, "/", "-") + "_" + time.Now().Format("20060102_150405")
	releaseDir := filepath.Join(layout.releasesDir, releaseName)
	_ = os.RemoveAll(releaseDir)
	if err := os.MkdirAll(releaseDir, 0o755); err != nil {
		return err
	}
	if err := copyReleaseTree(sourceDir, releaseDir); err != nil {
		return err
	}
	_ = os.RemoveAll(filepath.Join(releaseDir, "storage"))
	if err := os.Symlink(filepath.Join(layout.sharedDir, "storage"), filepath.Join(releaseDir, "storage")); err != nil {
		return err
	}
	exampleEnvPath := filepath.Join(releaseDir, "config", "panel.env.example")
	if _, err := os.Stat(layout.panelEnvPath); os.IsNotExist(err) {
		if _, statErr := os.Stat(exampleEnvPath); statErr == nil {
			if err := copyFile(exampleEnvPath, layout.panelEnvPath, 0o600); err != nil {
				return err
			}
		}
	}
	templatePath := filepath.Join(releaseDir, "deploy", "systemd", "mxinhy-panel.service")
	binaryPath := filepath.Join(releaseDir, "build", "panel", "mxinhy-panel")
	if err := rewritePanelServiceUnit(templatePath, layout.panelServiceName, binaryPath, layout.panelEnvPath, releaseDir); err != nil {
		return err
	}
	_ = os.Remove(layout.currentLink)
	if err := os.Symlink(releaseDir, layout.currentLink); err != nil {
		return err
	}
	if err := restartPanelService(layout.panelServiceName); err != nil {
		return err
	}
	return pruneReleaseDirs(layout.releasesDir, releaseDir, defaultUpgradeKeepReleases)
}

func rewritePanelServiceUnit(templatePath string, serviceName string, binaryPath string, envPath string, workdir string) error {
	content, err := os.ReadFile(templatePath)
	if err != nil {
		return err
	}
	replacer := strings.NewReplacer(
		"{{PANEL_INSTALL_PATH}}", binaryPath,
		"{{PANEL_ENV_PATH}}", envPath,
		"{{PANEL_WORKDIR}}", workdir,
	)
	unitPath := filepath.Join("/etc/systemd/system", serviceName+".service")
	if err := os.WriteFile(unitPath, []byte(replacer.Replace(string(content))), 0o644); err != nil {
		return err
	}
	if err := runCommandChecked("systemctl", "daemon-reload"); err != nil {
		return err
	}
	if err := runCommandChecked("systemctl", "enable", serviceName); err != nil {
		return err
	}
	return nil
}

func restartPanelService(serviceName string) error {
	return runCommandChecked("systemctl", "restart", serviceName)
}

func pruneReleaseDirs(releasesDir string, currentReleaseDir string, keep int) error {
	entries, err := os.ReadDir(releasesDir)
	if err != nil {
		return err
	}
	type releaseItem struct {
		path    string
		modTime time.Time
	}
	items := make([]releaseItem, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		items = append(items, releaseItem{
			path:    filepath.Join(releasesDir, entry.Name()),
			modTime: info.ModTime(),
		})
	}
	if len(items) <= keep {
		return nil
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].modTime.After(items[j].modTime)
	})
	for _, item := range items[keep:] {
		if filepath.Clean(item.path) == filepath.Clean(currentReleaseDir) {
			continue
		}
		if err := os.RemoveAll(item.path); err != nil {
			return err
		}
	}
	return nil
}

func replaceReleaseTree(sourceDir string, targetRoot string) error {
	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if name == "config" || name == "storage" {
			continue
		}
		target := filepath.Join(targetRoot, name)
		if err := os.RemoveAll(target); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return copyReleaseTree(sourceDir, targetRoot)
}

func copyFile(src string, dst string, mode os.FileMode) error {
	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer source.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	target, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer target.Close()
	_, err = io.Copy(target, source)
	return err
}

func runCommandChecked(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run()
}

func compareVersion(left string, right string) int {
	normalize := func(v string) []string { return strings.Split(strings.TrimSpace(v), ".") }
	l := normalize(left)
	r := normalize(right)
	maxLen := len(l)
	if len(r) > maxLen {
		maxLen = len(r)
	}
	for i := 0; i < maxLen; i++ {
		li, ri := 0, 0
		if i < len(l) {
			li, _ = strconv.Atoi(l[i])
		}
		if i < len(r) {
			ri, _ = strconv.Atoi(r[i])
		}
		if li > ri {
			return 1
		}
		if li < ri {
			return -1
		}
	}
	return 0
}

func downloadToFile(url string, filePath string, userAgent string) error {
	client := &http.Client{Timeout: 120 * time.Second}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return errors.New("download failed")
	}
	file, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(file, resp.Body)
	return err
}

func untarGz(archivePath string, targetDir string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()
	gzr, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzr.Close()
	tr := tar.NewReader(gzr)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		targetPath := filepath.Join(targetDir, header.Name)
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
				return err
			}
			file, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(file, tr); err != nil {
				file.Close()
				return err
			}
			file.Close()
		}
	}
}

func firstChildDirectory(root string) (string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			return filepath.Join(root, entry.Name()), nil
		}
	}
	return "", errors.New("not found")
}

func copyReleaseTree(sourceDir string, rootDir string) error {
	return filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(sourceDir, path)
		if err != nil || rel == "." {
			return err
		}
		if rel == "config" || rel == "storage" || strings.HasPrefix(rel, "config"+string(os.PathSeparator)) || strings.HasPrefix(rel, "storage"+string(os.PathSeparator)) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(rootDir, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		src, err := os.Open(path)
		if err != nil {
			return err
		}
		defer src.Close()
		dst, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
		if err != nil {
			return err
		}
		defer dst.Close()
		_, err = io.Copy(dst, src)
		return err
	})
}

func copyDir(src string, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, info.Mode()); err != nil {
		return err
	}
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil || rel == "." {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		sourceFile, err := os.Open(path)
		if err != nil {
			return err
		}
		defer sourceFile.Close()
		targetFile, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
		if err != nil {
			return err
		}
		defer targetFile.Close()
		_, err = io.Copy(targetFile, sourceFile)
		return err
	})
}
